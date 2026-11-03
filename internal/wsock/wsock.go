// Package wsock is a small RFC 6455 WebSocket client and server-side accept,
// written on the standard library so the binary stays pure Go. It supports
// text and binary messages, fragmentation, ping/pong, close, a message size
// cap and context deadlines. It does not support extensions (no compression).
package wsock

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/rayhanhanaputra/crosswire/internal/guard"
)

// Message types.
const (
	Text   = 1
	Binary = 2
)

const (
	opCont  = 0
	opClose = 8
	opPing  = 9
	opPong  = 10

	guid = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

	// DefaultMaxMessage caps a received message at 1 MiB.
	DefaultMaxMessage = 1 << 20
)

// ErrClosed is returned by Read after the peer sent a close frame.
var ErrClosed = errors.New("websocket closed by peer")

// Options tune Dial.
type Options struct {
	Subprotocol string
	MaxMessage  int // received message cap in bytes; default DefaultMaxMessage
	// Dial replaces the default guarded dialer. Tests of the dialer itself use it.
	Dial func(ctx context.Context, network, addr string) (net.Conn, error)
	// TLSConfig is used for wss. Default: system roots, ServerName from the URL.
	TLSConfig *tls.Config
}

// Conn is one WebSocket connection. Reads and writes may run concurrently with
// each other; writes are serialised.
type Conn struct {
	c       net.Conn
	br      *bufio.Reader
	client  bool
	max     int
	wmu     sync.Mutex
	closeMu sync.Once
}

// Dial opens a WebSocket to ws:// or wss://. Extra headers are sent on the
// upgrade request. Redirects are not followed. The destination goes through
// the guard's address checks (public addresses are allowed).
func Dial(ctx context.Context, rawURL string, header http.Header, opt Options) (*Conn, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "ws" && u.Scheme != "wss") || u.Host == "" {
		return nil, errors.New("websocket URL must be absolute ws:// or wss://")
	}
	port := u.Port()
	if port == "" {
		port = "80"
		if u.Scheme == "wss" {
			port = "443"
		}
	}
	dial := opt.Dial
	if dial == nil {
		dial = guard.DialContext(true, 10*time.Second)
	}
	raw, err := dial(ctx, "tcp", net.JoinHostPort(u.Hostname(), port))
	if err != nil {
		return nil, err
	}
	// The handshake and every later read honour the context.
	stop := context.AfterFunc(ctx, func() { _ = raw.SetDeadline(time.Unix(1, 0)) })
	defer stop()
	if dl, ok := ctx.Deadline(); ok {
		_ = raw.SetDeadline(dl)
	}
	conn := raw
	if u.Scheme == "wss" {
		cfg := opt.TLSConfig
		if cfg == nil {
			cfg = &tls.Config{MinVersion: tls.VersionTLS12}
		} else {
			cfg = cfg.Clone()
		}
		if cfg.ServerName == "" {
			cfg.ServerName = u.Hostname()
		}
		tc := tls.Client(raw, cfg)
		if err := tc.HandshakeContext(ctx); err != nil {
			_ = raw.Close()
			return nil, fmt.Errorf("tls handshake: %w", err)
		}
		conn = tc
	}
	keyRaw := make([]byte, 16)
	_, _ = rand.Read(keyRaw)
	key := base64.StdEncoding.EncodeToString(keyRaw)
	path := u.RequestURI()
	if path == "" {
		path = "/"
	}
	var req strings.Builder
	fmt.Fprintf(&req, "GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n", path, u.Host, key)
	if opt.Subprotocol != "" {
		fmt.Fprintf(&req, "Sec-WebSocket-Protocol: %s\r\n", opt.Subprotocol)
	}
	for k, vs := range header {
		switch strings.ToLower(k) {
		case "host", "upgrade", "connection", "sec-websocket-key", "sec-websocket-version":
			continue
		}
		for _, v := range vs {
			if strings.ContainsAny(k+v, "\r\n") {
				_ = conn.Close()
				return nil, errors.New("header contains a line break")
			}
			fmt.Fprintf(&req, "%s: %s\r\n", k, v)
		}
	}
	req.WriteString("\r\n")
	if _, err := io.WriteString(conn, req.String()); err != nil {
		_ = conn.Close()
		return nil, err
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodGet})
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("websocket handshake: %w", err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		_ = conn.Close()
		return nil, fmt.Errorf("websocket handshake refused: HTTP %d", resp.StatusCode)
	}
	if resp.Header.Get("Sec-WebSocket-Accept") != acceptKey(key) {
		_ = conn.Close()
		return nil, errors.New("websocket handshake: bad Sec-WebSocket-Accept")
	}
	max := opt.MaxMessage
	if max <= 0 {
		max = DefaultMaxMessage
	}
	_ = conn.SetDeadline(time.Time{})
	return &Conn{c: conn, br: br, client: true, max: max}, nil
}

// Accept upgrades a server request. It is used by the in-process fakes.
func Accept(w http.ResponseWriter, r *http.Request, maxMessage int) (*Conn, error) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") || !strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") {
		http.Error(w, "websocket upgrade required", http.StatusBadRequest)
		return nil, errors.New("not a websocket upgrade")
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	hj, ok := w.(http.Hijacker)
	if key == "" || !ok {
		http.Error(w, "cannot upgrade", http.StatusInternalServerError)
		return nil, errors.New("cannot hijack")
	}
	c, rw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}
	resp := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " + acceptKey(key) + "\r\n"
	if p := r.Header.Get("Sec-WebSocket-Protocol"); p != "" {
		resp += "Sec-WebSocket-Protocol: " + strings.TrimSpace(strings.Split(p, ",")[0]) + "\r\n"
	}
	resp += "\r\n"
	if _, err := rw.WriteString(resp); err != nil {
		_ = c.Close()
		return nil, err
	}
	if err := rw.Flush(); err != nil {
		_ = c.Close()
		return nil, err
	}
	if maxMessage <= 0 {
		maxMessage = DefaultMaxMessage
	}
	return &Conn{c: c, br: rw.Reader, client: false, max: maxMessage}, nil
}

func acceptKey(key string) string {
	h := sha1.Sum([]byte(key + guid))
	return base64.StdEncoding.EncodeToString(h[:])
}

// WriteText sends a text message.
func (c *Conn) WriteText(ctx context.Context, b []byte) error { return c.write(ctx, Text, b) }

// WriteBinary sends a binary message.
func (c *Conn) WriteBinary(ctx context.Context, b []byte) error { return c.write(ctx, Binary, b) }

func (c *Conn) write(ctx context.Context, op int, payload []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	stop := context.AfterFunc(ctx, func() { _ = c.c.SetWriteDeadline(time.Unix(1, 0)) })
	defer stop()
	if dl, ok := ctx.Deadline(); ok {
		_ = c.c.SetWriteDeadline(dl)
	} else {
		_ = c.c.SetWriteDeadline(time.Time{})
	}
	return c.writeFrame(true, op, payload)
}

func (c *Conn) writeFrame(fin bool, op int, payload []byte) error {
	var hdr [14]byte
	n := 2
	hdr[0] = byte(op)
	if fin {
		hdr[0] |= 0x80
	}
	maskBit := byte(0)
	if c.client {
		maskBit = 0x80
	}
	switch l := len(payload); {
	case l < 126:
		hdr[1] = maskBit | byte(l)
	case l < 1<<16:
		hdr[1] = maskBit | 126
		binary.BigEndian.PutUint16(hdr[2:], uint16(l))
		n = 4
	default:
		hdr[1] = maskBit | 127
		binary.BigEndian.PutUint64(hdr[2:], uint64(l))
		n = 10
	}
	out := payload
	if c.client {
		var mask [4]byte
		_, _ = rand.Read(mask[:])
		copy(hdr[n:], mask[:])
		n += 4
		out = make([]byte, len(payload))
		for i, b := range payload {
			out[i] = b ^ mask[i%4]
		}
	}
	if _, err := c.c.Write(hdr[:n]); err != nil {
		return err
	}
	_, err := c.c.Write(out)
	return err
}

// Read returns the next complete data message. Pings are answered and pongs
// skipped. A close frame returns ErrClosed. A message over the cap is an error.
func (c *Conn) Read(ctx context.Context) (typ int, data []byte, err error) {
	stop := context.AfterFunc(ctx, func() { _ = c.c.SetReadDeadline(time.Unix(1, 0)) })
	defer stop()
	if dl, ok := ctx.Deadline(); ok {
		_ = c.c.SetReadDeadline(dl)
	} else {
		_ = c.c.SetReadDeadline(time.Time{})
	}
	var buf []byte
	msgType := 0
	for {
		fin, op, payload, err := c.readFrame()
		if err != nil {
			if ctx.Err() != nil {
				return 0, nil, ctx.Err()
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				return 0, nil, context.DeadlineExceeded
			}
			return 0, nil, err
		}
		switch op {
		case opPing:
			c.wmu.Lock()
			_ = c.writeFrame(true, opPong, payload)
			c.wmu.Unlock()
			continue
		case opPong:
			continue
		case opClose:
			c.wmu.Lock()
			_ = c.writeFrame(true, opClose, payload)
			c.wmu.Unlock()
			return 0, nil, ErrClosed
		case Text, Binary:
			if msgType != 0 {
				return 0, nil, errors.New("websocket: new message inside a fragmented one")
			}
			msgType = op
		case opCont:
			if msgType == 0 {
				return 0, nil, errors.New("websocket: unexpected continuation frame")
			}
		default:
			return 0, nil, fmt.Errorf("websocket: unknown opcode %d", op)
		}
		if len(buf)+len(payload) > c.max {
			return 0, nil, fmt.Errorf("websocket message exceeds %d bytes", c.max)
		}
		buf = append(buf, payload...)
		if fin {
			return msgType, buf, nil
		}
	}
}

func (c *Conn) readFrame() (fin bool, op int, payload []byte, err error) {
	var h [2]byte
	if _, err = io.ReadFull(c.br, h[:]); err != nil {
		return
	}
	fin = h[0]&0x80 != 0
	if h[0]&0x70 != 0 {
		return false, 0, nil, errors.New("websocket: reserved bits set (extensions are not supported)")
	}
	op = int(h[0] & 0x0F)
	masked := h[1]&0x80 != 0
	l := uint64(h[1] & 0x7F)
	switch l {
	case 126:
		var b [2]byte
		if _, err = io.ReadFull(c.br, b[:]); err != nil {
			return
		}
		l = uint64(binary.BigEndian.Uint16(b[:]))
	case 127:
		var b [8]byte
		if _, err = io.ReadFull(c.br, b[:]); err != nil {
			return
		}
		l = binary.BigEndian.Uint64(b[:])
	}
	if l > uint64(c.max)+1024 {
		return false, 0, nil, fmt.Errorf("websocket frame of %d bytes exceeds the %d byte cap", l, c.max)
	}
	var mask [4]byte
	if masked {
		if _, err = io.ReadFull(c.br, mask[:]); err != nil {
			return
		}
	}
	payload = make([]byte, l)
	if _, err = io.ReadFull(c.br, payload); err != nil {
		return
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return
}

// Close sends a close frame (best effort) and closes the socket.
func (c *Conn) Close() error {
	var err error
	c.closeMu.Do(func() {
		c.wmu.Lock()
		_ = c.c.SetWriteDeadline(time.Now().Add(time.Second))
		_ = c.writeFrame(true, opClose, []byte{0x03, 0xE8})
		c.wmu.Unlock()
		err = c.c.Close()
	})
	return err
}
