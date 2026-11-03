package connector

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/rayhanhanaputra/crosswire/internal/evidence"
	"github.com/rayhanhanaputra/crosswire/internal/guard"
	"github.com/rayhanhanaputra/crosswire/internal/model"
)

// A minimal MQTT 3.1.1 client: CONNECT, SUBSCRIBE, PUBLISH (QoS 0 and 1),
// PUBACK, PING and DISCONNECT. It publishes one request and waits for the one
// reply that belongs to this trial. No retained messages, no will, no QoS 2.

const (
	maxMQTTRequestBytes = 256 << 10
	mqttKeepAlive       = 60

	mqttConnect    = 1
	mqttConnack    = 2
	mqttPublish    = 3
	mqttPuback     = 4
	mqttSubscribe  = 8
	mqttSuback     = 9
	mqttPingreq    = 12
	mqttPingresp   = 13
	mqttDisconnect = 14
)

type mqttPacket struct {
	typ     byte
	flags   byte
	payload []byte
}

func mqttRemainingLength(n int) []byte {
	var out []byte
	for {
		b := byte(n % 128)
		n /= 128
		if n > 0 {
			b |= 0x80
		}
		out = append(out, b)
		if n == 0 {
			return out
		}
	}
}

func mqttString(s string) []byte {
	out := make([]byte, 2, 2+len(s))
	binary.BigEndian.PutUint16(out, uint16(len(s)))
	return append(out, s...)
}

func mqttFrame(typ, flags byte, payload []byte) []byte {
	out := []byte{typ<<4 | flags&0x0F}
	out = append(out, mqttRemainingLength(len(payload))...)
	return append(out, payload...)
}

func readMQTTPacket(r *bufio.Reader, max int) (mqttPacket, error) {
	var p mqttPacket
	b, err := r.ReadByte()
	if err != nil {
		return p, err
	}
	p.typ, p.flags = b>>4, b&0x0F
	n, mult := 0, 1
	for i := 0; ; i++ {
		d, err := r.ReadByte()
		if err != nil {
			return p, err
		}
		n += int(d&0x7F) * mult
		if d&0x80 == 0 {
			break
		}
		mult *= 128
		if i >= 3 {
			return p, errors.New("mqtt: malformed remaining length")
		}
	}
	if n > max {
		return p, fmt.Errorf("mqtt packet of %d bytes exceeds the %d byte cap", n, max)
	}
	p.payload = make([]byte, n)
	_, err = io.ReadFull(r, p.payload)
	return p, err
}

func mqttConnackError(code byte) string {
	switch code {
	case 1:
		return "unacceptable protocol version"
	case 2:
		return "client identifier rejected"
	case 3:
		return "server unavailable"
	case 4:
		return "bad user name or password"
	case 5:
		return "not authorized"
	}
	return fmt.Sprintf("return code %d", code)
}

func expandTopic(topic, canary string) string { return strings.ReplaceAll(topic, "{{canary}}", canary) }

// CheckMQTTTopic reports why a topic is not usable: empty, too long, or a wildcard.
func CheckMQTTTopic(topic string) error {
	switch {
	case topic == "":
		return errors.New("topic is empty")
	case len(topic) > 512:
		return errors.New("topic is longer than 512 bytes")
	case strings.ContainsAny(topic, "+#\x00"):
		return errors.New("topic must be exact and cannot contain wildcards")
	}
	return nil
}

func mqttMatches(cfg model.MQTTConfig, payload []byte, canary string) bool {
	switch cfg.Correlate {
	case "first":
		return true
	case "path":
		var doc any
		if json.Unmarshal(payload, &doc) != nil {
			return false
		}
		v, ok := lookup(doc, cfg.CorrelationPath)
		if !ok {
			return false
		}
		return evidence.Carries(asString(v), canary)
	}
	return strings.Contains(strings.ToLower(string(payload)), strings.ToLower(canary))
}

// sendMQTT publishes the request to the publish topic and returns the first
// reply on the response topic that matches the trial's correlation rule.
func sendMQTT(ctx context.Context, t model.Target, endpoint string, body map[string]any, in Input) (model.Exchange, error) {
	ex := model.Exchange{Request: body}
	start := time.Now()
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "mqtt" && u.Scheme != "mqtts") || u.Hostname() == "" {
		return fail(ex, start, errors.New("mqtt target endpoint must be mqtt://host:port or mqtts://host:port"))
	}
	port := u.Port()
	if port == "" {
		port = "1883"
		if u.Scheme == "mqtts" {
			port = "8883"
		}
	}
	cfg := t.MQTT
	pubTopic, respTopic := expandTopic(cfg.PublishTopic, in.Canary), expandTopic(cfg.ResponseTopic, in.Canary)
	if err := CheckMQTTTopic(pubTopic); err != nil {
		return fail(ex, start, fmt.Errorf("mqtt publish topic: %w", err))
	}
	if err := CheckMQTTTopic(respTopic); err != nil {
		return fail(ex, start, fmt.Errorf("mqtt response topic: %w", err))
	}
	qos := byte(1)
	if cfg.QoS != nil {
		if *cfg.QoS != 0 && *cfg.QoS != 1 {
			return fail(ex, start, errors.New("mqtt qos must be 0 or 1"))
		}
		qos = byte(*cfg.QoS)
	}
	correlate := cfg.Correlate
	if correlate == "" {
		correlate = "nonce"
	}
	cfg.Correlate = correlate
	if correlate == "nonce" && in.Canary == "" {
		return fail(ex, start, errors.New("mqtt correlation by nonce needs a trial canary"))
	}
	payload, _ := json.Marshal(body)
	if len(payload) > maxMQTTRequestBytes {
		return fail(ex, start, errors.New("rendered request exceeds 256 KiB"))
	}
	var user, pass string
	if cfg.UsernameEnv != "" {
		if user = os.Getenv(cfg.UsernameEnv); user == "" {
			return fail(ex, start, fmt.Errorf("mqtt username: environment variable %s is not set", cfg.UsernameEnv))
		}
	}
	if cfg.PasswordEnv != "" {
		if cfg.UsernameEnv == "" {
			return fail(ex, start, errors.New("mqtt password needs a username"))
		}
		if pass = os.Getenv(cfg.PasswordEnv); pass == "" {
			return fail(ex, start, fmt.Errorf("mqtt password: environment variable %s is not set", cfg.PasswordEnv))
		}
	}
	ex.Wire = map[string]any{"transport": "mqtt", "broker": net.JoinHostPort(u.Hostname(), port), "tls": u.Scheme == "mqtts",
		"publish_topic": pubTopic, "response_topic": respTopic, "qos": int(qos), "correlate": correlate, "retain": false}

	ctx, cancel := context.WithTimeout(ctx, Timeout(t))
	defer cancel()
	raw, err := guard.DialContext(false, 10*time.Second)(ctx, "tcp", net.JoinHostPort(u.Hostname(), port))
	if err != nil {
		return fail(ex, start, fmt.Errorf("mqtt broker connection failed: %w", err))
	}
	conn := raw
	if u.Scheme == "mqtts" {
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
			return fail(ex, start, fmt.Errorf("mqtt broker TLS failed: %w", err))
		}
		conn = c
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Unix(1, 0)) })
	defer stop()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	br := bufio.NewReader(conn)
	timedOut := func(err error) error {
		if ctx.Err() != nil || isTimeout(err) {
			return errors.New("mqtt response timed out")
		}
		return err
	}

	idb := make([]byte, 6)
	_, _ = rand.Read(idb)
	clientID := "cw-" + hex.EncodeToString(idb)
	flags := byte(0x02) // clean session
	vh := append(mqttString("MQTT"), 4, 0, 0, 0)
	if user != "" {
		flags |= 0x80
	}
	if pass != "" {
		flags |= 0x40
	}
	vh[7] = flags
	binary.BigEndian.PutUint16(vh[8:], mqttKeepAlive)
	pl := mqttString(clientID)
	if user != "" {
		pl = append(pl, mqttString(user)...)
	}
	if pass != "" {
		pl = append(pl, mqttString(pass)...)
	}
	if _, err := conn.Write(mqttFrame(mqttConnect, 0, append(vh, pl...))); err != nil {
		return fail(ex, start, fmt.Errorf("mqtt connect failed: %w", timedOut(err)))
	}
	p, err := readMQTTPacket(br, maxResponseBytes)
	if err != nil {
		return fail(ex, start, fmt.Errorf("mqtt connect failed: %w", timedOut(err)))
	}
	if p.typ != mqttConnack || len(p.payload) != 2 {
		return fail(ex, start, errors.New("mqtt broker did not answer CONNECT with CONNACK"))
	}
	if p.payload[1] != 0 {
		return fail(ex, start, fmt.Errorf("mqtt broker refused the connection: %s", mqttConnackError(p.payload[1])))
	}
	defer func() { _, _ = conn.Write(mqttFrame(mqttDisconnect, 0, nil)) }()

	// Subscribe before publishing so the reply cannot be missed.
	sub := binary.BigEndian.AppendUint16(nil, 1)
	sub = append(sub, mqttString(respTopic)...)
	sub = append(sub, qos)
	if _, err := conn.Write(mqttFrame(mqttSubscribe, 0x02, sub)); err != nil {
		return fail(ex, start, fmt.Errorf("mqtt subscribe failed: %w", timedOut(err)))
	}
	for {
		p, err = readMQTTPacket(br, maxResponseBytes)
		if err != nil {
			return fail(ex, start, fmt.Errorf("mqtt subscribe failed: %w", timedOut(err)))
		}
		if p.typ == mqttSuback {
			if len(p.payload) < 3 || p.payload[2] == 0x80 {
				return fail(ex, start, errors.New("mqtt broker refused the response topic subscription"))
			}
			break
		}
	}

	pub := mqttString(pubTopic)
	if qos > 0 {
		pub = binary.BigEndian.AppendUint16(pub, 2)
	}
	pub = append(pub, payload...)
	if _, err := conn.Write(mqttFrame(mqttPublish, qos<<1, pub)); err != nil {
		return fail(ex, start, fmt.Errorf("mqtt publish failed: %w", timedOut(err)))
	}
	acked := qos == 0
	ping := time.NewTicker(mqttKeepAlive * time.Second / 2)
	defer ping.Stop()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-ping.C:
				_, _ = conn.Write(mqttFrame(mqttPingreq, 0, nil))
			}
		}
	}()
	for {
		p, err = readMQTTPacket(br, maxResponseBytes+4096)
		if err != nil {
			if !acked {
				return fail(ex, start, fmt.Errorf("mqtt publish was not acknowledged: %w", timedOut(err)))
			}
			return fail(ex, start, timedOut(err))
		}
		switch p.typ {
		case mqttPuback:
			acked = true
		case mqttPublish:
			if len(p.payload) < 2 {
				continue
			}
			tl := int(binary.BigEndian.Uint16(p.payload))
			if len(p.payload) < 2+tl {
				continue
			}
			topic := string(p.payload[2 : 2+tl])
			rest := p.payload[2+tl:]
			pqos := (p.flags >> 1) & 3
			if pqos > 0 {
				if len(rest) < 2 {
					continue
				}
				_, _ = conn.Write(mqttFrame(mqttPuback, 0, rest[:2]))
				rest = rest[2:]
			}
			if topic != respTopic {
				continue
			}
			if len(rest) > maxResponseBytes {
				return fail(ex, start, errors.New("mqtt response exceeds 2 MiB"))
			}
			if !mqttMatches(cfg, rest, in.Canary) {
				continue
			}
			ex.DurationMs = int(time.Since(start).Milliseconds())
			Finish(t, &ex, strings.ToValidUTF8(string(rest), "�"))
			return ex, nil
		}
	}
}
