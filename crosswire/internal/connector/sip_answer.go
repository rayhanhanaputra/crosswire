package connector

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/signal"
)

// Answerer is the far end of Dial: a minimal SIP user agent server. It
// answers an INVITE with 200 and an SDP offer of PCMU/8000, records the
// caller's RTP until the caller hangs up or stops sending for Idle, hands the
// audio to Handler as a WAV, optionally plays the Handler's reply WAV back as
// RTP, and then sends BYE. Plain UDP, no registration, no authentication: it
// is meant to be bound to a lab interface in front of a reference target.
type Answerer struct {
	// Handler receives the caller's audio. It may return a WAV to play back,
	// or nil to hang up at once.
	Handler func(ctx context.Context, wav []byte) ([]byte, error)
	// Idle is how long the caller may be silent before the call is treated
	// as finished. Default 2 s.
	Idle time.Duration
	// NoMedia hangs up a call that never sends audio. Default 15 s.
	NoMedia time.Duration
	// MaxCall bounds one call. Default 90 s.
	MaxCall time.Duration
	Logf    func(format string, a ...any)

	conn    *net.UDPConn
	localIP string // advertised in SDP; empty means route to the caller

	mu    sync.Mutex
	calls map[string]*inCall
}

type inCall struct {
	callID    string
	from, to  string // headers as the caller sent them (to without our tag)
	toTag     string
	via       string
	contact   string
	peer      *net.UDPAddr // SIP source of the INVITE
	rtp       *net.UDPConn
	callerRTP *net.UDPAddr
	ok200     []byte
	byeSeen   chan struct{} // caller sent BYE
	byeAcked  chan struct{} // caller answered our BYE
	byeOnce   sync.Once
	ackOnce   sync.Once
}

// NewAnswerer binds the SIP socket. listen is host:port; port 0 picks one.
func NewAnswerer(listen string, h func(ctx context.Context, wav []byte) ([]byte, error)) (*Answerer, error) {
	addr, err := net.ResolveUDPAddr("udp", listen)
	if err != nil {
		return nil, fmt.Errorf("sip listen %q: %w", listen, err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("sip listen: %w", err)
	}
	a := &Answerer{Handler: h, conn: conn, calls: map[string]*inCall{}}
	if addr.IP != nil && !addr.IP.IsUnspecified() {
		a.localIP = addr.IP.String()
	}
	return a, nil
}

// Addr is the bound SIP address.
func (a *Answerer) Addr() *net.UDPAddr { return a.conn.LocalAddr().(*net.UDPAddr) }

func (a *Answerer) logf(format string, v ...any) {
	if a.Logf != nil {
		a.Logf(format, v...)
	}
}

func or(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return def
}

// Serve answers calls until ctx is cancelled, then closes the socket.
func (a *Answerer) Serve(ctx context.Context) error {
	go func() { <-ctx.Done(); a.conn.Close() }()
	var wg sync.WaitGroup
	defer wg.Wait()
	buf := make([]byte, 65535)
	for {
		n, from, err := a.conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		m, perr := parseSIP(string(buf[:n]))
		if perr != nil {
			continue
		}
		if m.method == "" {
			a.onResponse(m)
			continue
		}
		switch m.method {
		case "INVITE":
			a.onInvite(ctx, &wg, m, from)
		case "BYE":
			a.reply(from, m, 200, "OK", nil, "", "")
			if c := a.call(m.get("Call-ID")); c != nil {
				c.byeOnce.Do(func() { close(c.byeSeen) })
			}
		case "CANCEL", "OPTIONS", "INFO", "NOTIFY", "UPDATE":
			a.reply(from, m, 200, "OK", nil, "", "")
		case "ACK":
		default:
			a.reply(from, m, 405, "Method Not Allowed", nil, "", "")
		}
	}
}

func (a *Answerer) call(id string) *inCall {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.calls[id]
}

func (a *Answerer) onResponse(m *sipMsg) {
	if !strings.Contains(strings.ToUpper(m.get("CSeq")), "BYE") {
		return
	}
	if c := a.call(m.get("Call-ID")); c != nil {
		c.ackOnce.Do(func() { close(c.byeAcked) })
	}
}

func (a *Answerer) reply(to *net.UDPAddr, req *sipMsg, code int, text string, extra []string, body, toTag string) {
	var b strings.Builder
	fmt.Fprintf(&b, "SIP/2.0 %d %s\r\n", code, text)
	toHdr := req.get("To")
	if toTag != "" && !strings.Contains(toHdr, "tag=") {
		toHdr += ";tag=" + toTag
	}
	fmt.Fprintf(&b, "Via: %s\r\nFrom: %s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: %s\r\n",
		req.get("Via"), req.get("From"), toHdr, req.get("Call-ID"), req.get("CSeq"))
	for _, e := range extra {
		b.WriteString(e + "\r\n")
	}
	if body != "" {
		b.WriteString("Content-Type: application/sdp\r\n")
	}
	fmt.Fprintf(&b, "Content-Length: %d\r\n\r\n%s", len(body), body)
	_, _ = a.conn.WriteToUDP([]byte(b.String()), to)
}

func offersPCMU(sdp string) bool {
	for _, line := range strings.Split(sdp, "\n") {
		if f := strings.Fields(strings.TrimSpace(line)); len(f) > 3 && f[0] == "m=audio" {
			for _, pt := range f[3:] {
				if pt == "0" {
					return true
				}
			}
		}
	}
	return false
}

func (a *Answerer) onInvite(ctx context.Context, wg *sync.WaitGroup, m *sipMsg, from *net.UDPAddr) {
	id := m.get("Call-ID")
	if c := a.call(id); c != nil { // retransmitted INVITE
		_, _ = a.conn.WriteToUDP(c.ok200, from)
		return
	}
	callerRTP, err := parseSDP(m.body)
	if err != nil || !offersPCMU(m.body) {
		a.reply(from, m, 488, "Not Acceptable Here", nil, "", randHex(4))
		return
	}
	ip := a.localIP
	if ip == "" {
		if ip, err = outboundIP(from); err != nil {
			a.reply(from, m, 500, "Server Error", nil, "", randHex(4))
			return
		}
	}
	rtp, err := listenRTP(ip)
	if err != nil {
		a.reply(from, m, 500, "Server Error", nil, "", randHex(4))
		return
	}
	c := &inCall{callID: id, from: m.get("From"), to: m.get("To"), toTag: randHex(4), via: m.get("Via"),
		contact: contactURI(m.get("Contact")), peer: from, rtp: rtp, callerRTP: callerRTP,
		byeSeen: make(chan struct{}), byeAcked: make(chan struct{})}
	if c.contact == "" {
		c.contact = "sip:" + from.String()
	}
	sdp := fmt.Sprintf("v=0\r\no=- %d %d IN IP4 %s\r\ns=crosswire\r\nc=IN IP4 %s\r\nt=0 0\r\nm=audio %d RTP/AVP 0\r\na=rtpmap:0 PCMU/8000\r\na=sendrecv\r\n",
		time.Now().Unix(), time.Now().Unix(), ip, ip, rtp.LocalAddr().(*net.UDPAddr).Port)
	contact := fmt.Sprintf("Contact: <sip:crosswire@%s:%d>", ip, a.Addr().Port)
	var b strings.Builder
	fmt.Fprintf(&b, "SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=%s\r\nCall-ID: %s\r\nCSeq: %s\r\n%s\r\nContent-Type: application/sdp\r\nContent-Length: %d\r\n\r\n%s",
		c.via, c.from, c.to, c.toTag, id, m.get("CSeq"), contact, len(sdp), sdp)
	c.ok200 = []byte(b.String())
	a.mu.Lock()
	a.calls[id] = c
	a.mu.Unlock()
	_, _ = a.conn.WriteToUDP(c.ok200, from)
	a.logf("sip: call %s answered from %s, caller media %s", id, from, callerRTP)
	wg.Add(1)
	go func() {
		defer wg.Done()
		a.run(ctx, c, ip)
	}()
}

// run records the caller, calls the handler, plays the reply and hangs up.
func (a *Answerer) run(ctx context.Context, c *inCall, ip string) {
	defer func() {
		c.rtp.Close()
		a.mu.Lock()
		delete(a.calls, c.callID)
		a.mu.Unlock()
	}()
	cctx, cancel := context.WithTimeout(ctx, or(a.MaxCall, 90*time.Second))
	defer cancel()
	idle, noMedia := or(a.Idle, 2*time.Second), or(a.NoMedia, 15*time.Second)

	var got []byte
	started := time.Now()
	var last time.Time
	buf := make([]byte, 2048)
	callerHungUp := false
record:
	for cctx.Err() == nil {
		select {
		case <-c.byeSeen:
			callerHungUp = true
			break record
		default:
		}
		_ = c.rtp.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		n, _, err := c.rtp.ReadFromUDP(buf)
		if err == nil && n > 12 && buf[0]>>6 == 2 {
			hdr := 12 + 4*int(buf[0]&0x0F)
			if buf[0]&0x10 != 0 && n >= hdr+4 {
				hdr += 4 + 4*int(uint16(buf[hdr+2])<<8|uint16(buf[hdr+3]))
			}
			if buf[1]&0x7F == 0 && n > hdr && len(got) < 8000*maxCallSecs {
				got = append(got, buf[hdr:n]...)
				last = time.Now()
			}
		}
		switch {
		case !last.IsZero() && time.Since(last) >= idle:
			break record
		case last.IsZero() && time.Since(started) >= noMedia:
			break record
		}
	}
	var reply []byte
	if len(got) >= 800 && a.Handler != nil { // at least 0.1 s of audio
		a.logf("sip: call %s: %.1f s of caller audio, running the agent", c.callID, float64(len(got))/8000)
		wav, err := a.Handler(cctx, signal.FromMuLaw8k(got))
		if err != nil {
			a.logf("sip: call %s: handler: %v", c.callID, err)
		}
		reply = wav
	} else {
		a.logf("sip: call %s: no usable audio (%d bytes)", c.callID, len(got))
	}
	if len(reply) > 0 && !callerHungUp {
		if ulaw, err := signal.ToMuLaw8k(reply); err == nil {
			sendRTP(cctx, c.rtp, c.callerRTP, ulaw, &Call{})
		}
	}
	if callerHungUp {
		return
	}
	select {
	case <-c.byeSeen:
		return
	default:
	}
	a.sendBye(c, ip)
}

func (a *Answerer) sendBye(c *inCall, ip string) {
	msg := fmt.Sprintf("BYE %s SIP/2.0\r\nVia: SIP/2.0/UDP %s:%d;branch=z9hG4bK%s;rport\r\nMax-Forwards: 70\r\nFrom: %s;tag=%s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: 1 BYE\r\nContent-Length: 0\r\n\r\n",
		c.contact, ip, a.Addr().Port, randHex(8), c.to, c.toTag, c.from, c.callID)
	for i := 0; i < 3; i++ {
		_, _ = a.conn.WriteToUDP([]byte(msg), c.peer)
		select {
		case <-c.byeAcked:
			return
		case <-c.byeSeen:
			return
		case <-time.After(500 * time.Millisecond):
		}
	}
	a.logf("sip: call %s: BYE was not acknowledged", c.callID)
}
