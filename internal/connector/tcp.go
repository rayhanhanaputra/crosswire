package connector

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rayhanhanaputra/crosswire/internal/guard"
	"github.com/rayhanhanaputra/crosswire/internal/model"
)

const (
	maxTCPRequestBytes = 1 << 20
	defaultTCPReply    = 64 << 10
	maxTCPReply        = 1 << 20
)

// testTLS lets tests trust a self-signed server. It is nil in production.
var testTLS *tls.Config

// ParseTerminator turns the terminator setting into bytes. Empty means "\n",
// "none" means no terminator, and \n \r \t \0 \xNN escapes are accepted.
func ParseTerminator(s string) ([]byte, error) {
	if s == "" {
		return []byte("\n"), nil
	}
	if strings.EqualFold(s, "none") {
		return nil, nil
	}
	var out []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' {
			out = append(out, c)
			continue
		}
		i++
		if i >= len(s) {
			return nil, errors.New("terminator ends with a lone backslash")
		}
		switch s[i] {
		case 'n':
			out = append(out, '\n')
		case 'r':
			out = append(out, '\r')
		case 't':
			out = append(out, '\t')
		case '0':
			out = append(out, 0)
		case '\\':
			out = append(out, '\\')
		case 'x':
			if i+2 >= len(s) {
				return nil, errors.New("terminator \\x needs two hex digits")
			}
			v, err := strconv.ParseUint(s[i+1:i+3], 16, 8)
			if err != nil {
				return nil, errors.New("terminator \\x needs two hex digits")
			}
			out = append(out, byte(v))
			i += 2
		default:
			return nil, fmt.Errorf("terminator escape \\%c is not supported", s[i])
		}
	}
	return out, nil
}

// sendTCP sends one request on a fresh connection and reads the reply as the
// target's TCPConfig says. Raw TCP is limited to loopback and private networks.
func sendTCP(ctx context.Context, t model.Target, endpoint string, body map[string]any, in Input) (model.Exchange, error) {
	ex := model.Exchange{Request: body}
	start := time.Now()
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "tcp" || u.Hostname() == "" || u.Port() == "" {
		return fail(ex, start, errors.New("tcp target endpoint must be tcp://host:port"))
	}
	cfg := t.TCP
	term, err := ParseTerminator(cfg.Terminator)
	if err != nil {
		return fail(ex, start, err)
	}
	framing := cfg.Framing
	if framing == "" {
		framing = "terminator"
	}
	if framing != "terminator" && framing != "length" {
		return fail(ex, start, fmt.Errorf("tcp framing %q: want terminator or length", framing))
	}
	readUntil := cfg.ReadUntil
	if readUntil == "" {
		readUntil = "terminator"
	}
	if framing == "length" && readUntil == "terminator" {
		readUntil = "length"
	}
	if readUntil == "terminator" && len(term) == 0 {
		readUntil = "idle"
	}
	switch readUntil {
	case "terminator", "length", "close", "idle", "none":
	default:
		return fail(ex, start, fmt.Errorf("tcp read_until %q: want terminator, close, idle or none", readUntil))
	}
	var payload []byte
	if cfg.Send == "raw" {
		payload = []byte(in.Content)
		ex.Request = map[string]any{"raw": in.Content}
	} else {
		payload, _ = json.Marshal(body)
	}
	if len(payload) > maxTCPRequestBytes {
		return fail(ex, start, errors.New("rendered request exceeds 1 MiB"))
	}
	maxReply := cfg.MaxBytes
	if maxReply <= 0 {
		maxReply = defaultTCPReply
	}
	if maxReply > maxTCPReply {
		maxReply = maxTCPReply
	}
	idle := time.Duration(cfg.IdleMs) * time.Millisecond
	if idle <= 0 {
		idle = 500 * time.Millisecond
	}
	ex.Wire = map[string]any{"transport": "tcp", "send": orDefault(cfg.Send, "json"), "framing": framing, "read_until": readUntil, "tls": cfg.TLS}

	ctx, cancel := context.WithTimeout(ctx, Timeout(t))
	defer cancel()
	raw, err := guard.DialContext(false, 10*time.Second)(ctx, "tcp", u.Host)
	if err != nil {
		return fail(ex, start, fmt.Errorf("tcp target connection failed: %w", err))
	}
	conn := raw
	if cfg.TLS {
		tc := testTLS
		if tc == nil {
			tc = &tls.Config{MinVersion: tls.VersionTLS12}
		} else {
			tc = tc.Clone()
		}
		if tc.ServerName == "" {
			tc.ServerName = u.Hostname()
		}
		c := tls.Client(raw, tc)
		if err := c.HandshakeContext(ctx); err != nil {
			_ = raw.Close()
			return fail(ex, start, fmt.Errorf("tcp target TLS failed: %w", err))
		}
		conn = c
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Unix(1, 0)) })
	defer stop()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}

	var frame []byte
	if framing == "length" {
		frame = make([]byte, 4, 4+len(payload))
		binary.BigEndian.PutUint32(frame, uint32(len(payload)))
		frame = append(frame, payload...)
	} else {
		frame = append(append([]byte(nil), payload...), term...)
	}
	if _, err := conn.Write(frame); err != nil {
		return fail(ex, start, fmt.Errorf("tcp send failed: %w", err))
	}
	ex.ResponseText = ""
	if readUntil == "none" {
		ex.DurationMs = int(time.Since(start).Milliseconds())
		ex.ResponseText = fmt.Sprintf("sent %d bytes; no reply is read (read_until none)", len(frame))
		return ex, nil
	}

	reply, err := readTCPReply(ctx, conn, readUntil, term, maxReply, idle)
	if err != nil {
		return fail(ex, start, err)
	}
	ex.DurationMs = int(time.Since(start).Milliseconds())
	Finish(t, &ex, strings.ToValidUTF8(string(reply), "�"))
	return ex, nil
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

// readTCPReply reads until the stop condition. A read that times out after
// some bytes arrived returns those bytes: a service that never closes or
// terminates its reply still answers. A timeout with nothing read is an error.
func readTCPReply(ctx context.Context, conn net.Conn, mode string, term []byte, max int, idle time.Duration) ([]byte, error) {
	var buf []byte
	tmp := make([]byte, 4096)
	if mode == "length" {
		var hdr [4]byte
		if _, err := io.ReadFull(conn, hdr[:]); err != nil {
			return nil, fmt.Errorf("tcp reply failed: %w", noResponse(ctx, err))
		}
		n := int(binary.BigEndian.Uint32(hdr[:]))
		if n < 0 || n > max {
			return nil, fmt.Errorf("tcp reply exceeds %d bytes", max)
		}
		buf = make([]byte, n)
		if _, err := io.ReadFull(conn, buf); err != nil {
			return nil, fmt.Errorf("tcp reply failed: %w", noResponse(ctx, err))
		}
		return buf, nil
	}
	for {
		if mode == "idle" && len(buf) > 0 {
			_ = conn.SetReadDeadline(time.Now().Add(idle))
		}
		n, err := conn.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if len(buf) > max {
			return nil, fmt.Errorf("tcp reply exceeds %d bytes", max)
		}
		if mode == "terminator" {
			if i := bytes.Index(buf, term); i >= 0 {
				return buf[:i], nil
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) || isTimeout(err) {
				if len(buf) == 0 {
					return nil, fmt.Errorf("tcp reply failed: %w", noResponse(ctx, err))
				}
				if mode == "terminator" {
					return bytes.TrimSuffix(buf, term), nil
				}
				return buf, nil
			}
			return nil, fmt.Errorf("tcp reply failed: %w", err)
		}
	}
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func noResponse(ctx context.Context, err error) error {
	if isTimeout(err) || ctx.Err() != nil {
		return errors.New("no response before the timeout")
	}
	if errors.Is(err, io.EOF) {
		return errors.New("connection closed with no response")
	}
	return err
}
