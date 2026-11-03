package fakes

import (
	"bufio"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
)

// MQTTMessage is one PUBLISH the broker received.
type MQTTMessage struct {
	Topic   string
	Payload []byte
	QoS     int
	Retain  bool
}

type mqttSub struct {
	filter string
	qos    byte
}

type mqttClient struct {
	c    net.Conn
	wmu  sync.Mutex
	subs []mqttSub
	next atomic.Uint32
}

func (c *mqttClient) write(b []byte) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_, _ = c.c.Write(b)
}

type mqttHook struct {
	filter string
	fn     func(topic string, payload []byte)
}

// MQTTBroker is a small MQTT 3.1.1 broker: CONNECT with optional
// username/password, SUBSCRIBE with + and # filters, PUBLISH at QoS 0 and 1.
type MQTTBroker struct {
	ln   net.Listener
	tls  bool
	mu   sync.Mutex
	cl   map[*mqttClient]struct{}
	hook []mqttHook
	log  []MQTTMessage
	user string
	pass string
	auth bool
	// Connects counts accepted CONNECT packets.
	Connects atomic.Int32
	// LastClientID is the client id of the last CONNECT.
	lastID atomic.Value
}

// NewMQTTBroker starts a plain broker on a loopback port.
func NewMQTTBroker() *MQTTBroker { return newBroker(nil) }

// NewMQTTBrokerTLS starts a broker that serves TLS.
func NewMQTTBrokerTLS(cfg *tls.Config) *MQTTBroker { return newBroker(cfg) }

func newBroker(cfg *tls.Config) *MQTTBroker {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	if cfg != nil {
		ln = tls.NewListener(ln, cfg)
	}
	b := &MQTTBroker{ln: ln, tls: cfg != nil, cl: map[*mqttClient]struct{}{}}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go b.serve(c)
		}
	}()
	return b
}

// RequireAuth makes CONNECT without this username and password fail.
func (b *MQTTBroker) RequireAuth(user, pass string) { b.auth, b.user, b.pass = true, user, pass }

// URL is the mqtt:// or mqtts:// address.
func (b *MQTTBroker) URL() string {
	if b.tls {
		return "mqtts://" + b.ln.Addr().String()
	}
	return "mqtt://" + b.ln.Addr().String()
}

// Close stops the broker and drops every client.
func (b *MQTTBroker) Close() {
	_ = b.ln.Close()
	b.mu.Lock()
	for c := range b.cl {
		_ = c.c.Close()
	}
	b.mu.Unlock()
}

// LastClientID is the client id sent by the most recent CONNECT.
func (b *MQTTBroker) LastClientID() string {
	v, _ := b.lastID.Load().(string)
	return v
}

// Published lists every message clients published.
func (b *MQTTBroker) Published() []MQTTMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]MQTTMessage(nil), b.log...)
}

// Subscribe registers an in-process subscriber.
func (b *MQTTBroker) Subscribe(filter string, fn func(topic string, payload []byte)) {
	b.mu.Lock()
	b.hook = append(b.hook, mqttHook{filter, fn})
	b.mu.Unlock()
}

// Publish delivers a message to subscribers as if a client had published it.
func (b *MQTTBroker) Publish(topic string, payload []byte) { b.route(topic, payload, nil) }

func topicMatches(filter, topic string) bool {
	f, t := strings.Split(filter, "/"), strings.Split(topic, "/")
	for i, seg := range f {
		if seg == "#" {
			return true
		}
		if i >= len(t) {
			return false
		}
		if seg != "+" && seg != t[i] {
			return false
		}
	}
	return len(f) == len(t)
}

func (b *MQTTBroker) route(topic string, payload []byte, from *mqttClient) {
	b.mu.Lock()
	var hooks []func(string, []byte)
	for _, h := range b.hook {
		if topicMatches(h.filter, topic) {
			hooks = append(hooks, h.fn)
		}
	}
	var targets []*mqttClient
	var qoss []byte
	for c := range b.cl {
		for _, s := range c.subs {
			if topicMatches(s.filter, topic) {
				targets = append(targets, c)
				qoss = append(qoss, s.qos)
				break
			}
		}
	}
	b.mu.Unlock()
	for i, c := range targets {
		body := append([]byte{byte(len(topic) >> 8), byte(len(topic))}, topic...)
		q := qoss[i]
		if q > 0 {
			body = binary.BigEndian.AppendUint16(body, uint16(c.next.Add(1)))
		}
		body = append(body, payload...)
		c.write(frame(3, q<<1, body))
	}
	for _, fn := range hooks {
		go fn(topic, payload)
	}
}

func frame(typ, flags byte, payload []byte) []byte {
	out := []byte{typ<<4 | flags&0x0F}
	n := len(payload)
	for {
		d := byte(n % 128)
		n /= 128
		if n > 0 {
			d |= 0x80
		}
		out = append(out, d)
		if n == 0 {
			break
		}
	}
	return append(out, payload...)
}

func readPacket(r *bufio.Reader) (typ, flags byte, payload []byte, err error) {
	h, err := r.ReadByte()
	if err != nil {
		return
	}
	typ, flags = h>>4, h&0x0F
	n, mult := 0, 1
	for {
		var d byte
		if d, err = r.ReadByte(); err != nil {
			return
		}
		n += int(d&0x7F) * mult
		if d&0x80 == 0 {
			break
		}
		mult *= 128
	}
	payload = make([]byte, n)
	_, err = io.ReadFull(r, payload)
	return
}

func readStr(p []byte) (string, []byte, error) {
	if len(p) < 2 {
		return "", nil, errors.New("short")
	}
	n := int(binary.BigEndian.Uint16(p))
	if len(p) < 2+n {
		return "", nil, errors.New("short")
	}
	return string(p[2 : 2+n]), p[2+n:], nil
}

func (b *MQTTBroker) serve(nc net.Conn) {
	c := &mqttClient{c: nc}
	defer func() {
		b.mu.Lock()
		delete(b.cl, c)
		b.mu.Unlock()
		_ = nc.Close()
	}()
	br := bufio.NewReader(nc)
	typ, _, p, err := readPacket(br)
	if err != nil || typ != 1 {
		return
	}
	name, rest, err := readStr(p)
	if err != nil || name != "MQTT" || len(rest) < 4 || rest[0] != 4 {
		c.write(frame(2, 0, []byte{0, 1}))
		return
	}
	flags := rest[1]
	id, rest, err := readStr(rest[4:])
	if err != nil {
		return
	}
	var user, pass string
	if flags&0x80 != 0 {
		user, rest, _ = readStr(rest)
	}
	if flags&0x40 != 0 {
		pass, _, _ = readStr(rest)
	}
	if b.auth && (user != b.user || pass != b.pass) {
		c.write(frame(2, 0, []byte{0, 4}))
		return
	}
	b.Connects.Add(1)
	b.lastID.Store(id)
	b.mu.Lock()
	b.cl[c] = struct{}{}
	b.mu.Unlock()
	c.write(frame(2, 0, []byte{0, 0}))
	for {
		typ, fl, p, err := readPacket(br)
		if err != nil {
			return
		}
		switch typ {
		case 8: // SUBSCRIBE
			if len(p) < 2 {
				return
			}
			pid := p[:2]
			rest := p[2:]
			var codes []byte
			for len(rest) > 0 {
				var f string
				f, rest, err = readStr(rest)
				if err != nil || len(rest) < 1 {
					return
				}
				q := rest[0]
				if q > 1 {
					q = 1
				}
				rest = rest[1:]
				b.mu.Lock()
				c.subs = append(c.subs, mqttSub{f, q})
				b.mu.Unlock()
				codes = append(codes, q)
			}
			c.write(frame(9, 0, append(append([]byte(nil), pid...), codes...)))
		case 3: // PUBLISH
			topic, rest, err := readStr(p)
			if err != nil {
				return
			}
			q := (fl >> 1) & 3
			if q > 0 {
				if len(rest) < 2 {
					return
				}
				c.write(frame(4, 0, rest[:2]))
				rest = rest[2:]
			}
			b.mu.Lock()
			b.log = append(b.log, MQTTMessage{Topic: topic, Payload: append([]byte(nil), rest...), QoS: int(q), Retain: fl&1 != 0})
			b.mu.Unlock()
			b.route(topic, rest, c)
		case 12:
			c.write(frame(13, 0, nil))
		case 14:
			return
		}
	}
}

// MQTTAgentOptions shapes the agent behind the broker.
type MQTTAgentOptions struct {
	Noise bool // publish an unrelated reply first
}

// NewMQTTAgent subscribes to in, and for every request publishes the agent's
// reply on out. With Noise it first publishes a reply for another session, to
// exercise correlation.
func NewMQTTAgent(b *MQTTBroker, in, out string, a *Agent, opt MQTTAgentOptions) {
	b.Subscribe(in, func(_ string, payload []byte) {
		if opt.Noise {
			b.Publish(out, []byte(`{"text":"reply for another session","session":"cw-00000000"}`))
		}
		b.Publish(out, a.Reply(string(payload)))
	})
}
