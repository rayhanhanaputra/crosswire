package connector

import (
	"bufio"
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rayhanhanaputra/crosswire/internal/model"
	"github.com/rayhanhanaputra/crosswire/internal/signal"
)

// SIP delivery: the speech artifact is played into a real call instead of
// posted as a file. The target is a SIP URI the operator configured (the
// agent's own SIP endpoint, a PBX extension, or a trunk that reaches a
// phone number). Audio goes out as G.711 μ-law over RTP, the agent's reply
// is recorded, and the sink remains the only evidence of what it did.
//
// Implemented in plain UDP so the binary stays dependency-free: INVITE with
// digest authentication, early media ignored, ACK, RTP both ways, BYE.

// Call is what one SIP exchange produced.
type Call struct {
	// Error is what the far end reported in the X-Crosswire-Error header of
	// its BYE, for example a handler failure on a reference target. It is
	// the first field so a marshalled failed call starts with {"error".
	Error           string   `json:"error,omitempty"`
	URI             string   `json:"uri"`
	Status          int      `json:"status"`
	Reason          string   `json:"reason"`
	Answered        bool     `json:"answered"`
	SentSeconds     float64  `json:"sent_seconds"`
	SentPackets     int      `json:"sent_packets"`
	ReceivedSeconds float64  `json:"received_seconds"`
	ReceivedPackets int      `json:"received_packets"`
	RemoteHungUp    bool     `json:"remote_hung_up"`
	Log             []string `json:"log"`
	// ReplyAudio is the far end's audio as a WAV; stored by the engine, not
	// serialised with the call.
	ReplyAudio []byte `json:"-"`
}

const (
	sipT1       = 500 * time.Millisecond
	sipTimeout  = 32 * time.Second
	rtpInterval = 20 * time.Millisecond
	rtpFrame    = 160 // samples per 20 ms at 8 kHz
	maxCallSecs = 300
)

// SessionHeader carries the trial nonce on the INVITE. A reference target
// copies it into the session of every sink event the call causes.
const SessionHeader = "X-Crosswire-Session"

// ErrorHeader is set on the answerer's BYE when its handler failed.
const ErrorHeader = "X-Crosswire-Error"

// Dial places the call, plays wav, listens for the far end, hangs up.
func Dial(ctx context.Context, cfg model.SIPConfig, uri string, wav []byte) (Call, error) {
	return DialSession(ctx, cfg, uri, wav, "")
}

// DialSession is Dial with a trial session sent in the X-Crosswire-Session
// header of the INVITE.
func DialSession(ctx context.Context, cfg model.SIPConfig, uri string, wav []byte, session string) (Call, error) {
	call := Call{URI: uri}
	ulaw, err := signal.ToMuLaw8k(wav)
	if err != nil {
		return call, fmt.Errorf("audio: %w", err)
	}
	host, port, user, err := parseSIPURI(uri)
	if err != nil {
		return call, err
	}
	remote, err := net.ResolveUDPAddr("udp", net.JoinHostPort(host, port))
	if err != nil {
		return call, fmt.Errorf("resolve %s: %w", host, err)
	}
	localIP := cfg.LocalIP
	if localIP == "" {
		localIP, err = outboundIP(remote)
		if err != nil {
			return call, err
		}
	}
	sipConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP(localIP)})
	if err != nil {
		return call, fmt.Errorf("sip socket: %w", err)
	}
	defer sipConn.Close()
	rtpConn, err := listenRTP(localIP)
	if err != nil {
		return call, err
	}
	defer rtpConn.Close()
	rtpPort := rtpConn.LocalAddr().(*net.UDPAddr).Port
	sipPort := sipConn.LocalAddr().(*net.UDPAddr).Port

	from := cfg.From
	if from == "" {
		from = "crosswire"
	}
	fromTag, callID := randHex(8), randHex(16)
	d := &dialog{conn: sipConn, remote: remote, localIP: localIP, sipPort: sipPort, uri: uri, user: user,
		from: from, fromTag: fromTag, callID: callID, cseq: 1, call: &call, session: session}
	sdp := fmt.Sprintf("v=0\r\no=- %d %d IN IP4 %s\r\ns=crosswire\r\nc=IN IP4 %s\r\nt=0 0\r\nm=audio %d RTP/AVP 0\r\na=rtpmap:0 PCMU/8000\r\na=sendrecv\r\n",
		time.Now().Unix(), time.Now().Unix(), localIP, localIP, rtpPort)

	deadline := sipTimeout
	if cfg.RingSeconds > 0 {
		deadline = time.Duration(cfg.RingSeconds) * time.Second
	}
	ictx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	resp, err := d.invite(ictx, sdp, cfg)
	if err != nil {
		return call, err
	}
	call.Status, call.Reason = resp.status, resp.reason
	if resp.status/100 != 2 {
		return call, fmt.Errorf("call not answered: %d %s", resp.status, resp.reason)
	}
	call.Answered = true
	rtpRemote, err := parseSDP(resp.body)
	if err != nil {
		_ = d.bye(ctx)
		return call, err
	}
	d.logf("answered; remote media %s", rtpRemote)

	listen := time.Duration(cfg.ListenSeconds) * time.Second
	if listen <= 0 {
		listen = 20 * time.Second
	}
	total := time.Duration(len(ulaw)/8000)*time.Second + listen + 5*time.Second
	if total > maxCallSecs*time.Second {
		total = maxCallSecs * time.Second
	}
	mctx, mcancel := context.WithTimeout(ctx, total)
	defer mcancel()

	// Far-end BYE ends the media phase early. The in-call reader must be
	// gone before our own BYE transaction reads the socket, or it swallows
	// the 200 and the BYE is retransmitted.
	byeCh := make(chan struct{}, 1)
	serveDone := make(chan struct{})
	go func() {
		defer close(serveDone)
		d.serveDuringCall(mctx, byeCh)
	}()

	var received []byte
	recvDone := make(chan struct{})
	go func() {
		defer close(recvDone)
		received = receiveRTP(mctx, rtpConn, &call)
	}()
	sendRTP(mctx, rtpConn, rtpRemote, ulaw, &call)
	select {
	case <-byeCh:
		call.RemoteHungUp = true
	case <-time.After(listen):
	case <-mctx.Done():
	}
	mcancel()
	<-recvDone
	<-serveDone
	call.ReceivedSeconds = float64(len(received)) / 8000
	if len(received) > 0 {
		call.ReplyAudio = signal.FromMuLaw8k(received)
	}
	if !call.RemoteHungUp {
		if err := d.bye(ctx); err != nil {
			d.logf("bye: %v", err)
		}
	}
	return call, nil
}

type dialog struct {
	conn               *net.UDPConn
	remote             *net.UDPAddr
	localIP            string
	sipPort            int
	uri, user, from    string
	fromTag, toTag     string
	callID             string
	cseq               int
	remoteTarget       string // Contact from the 2xx, else the request URI
	call               *Call
	lastInviteCSeq     int
	lastInviteBranch   string
	lastInviteResponse *sipMsg
	session            string
}

func (d *dialog) logf(format string, a ...any) {
	d.call.Log = append(d.call.Log, time.Now().UTC().Format("15:04:05.000")+" "+fmt.Sprintf(format, a...))
}

type sipMsg struct {
	status  int
	reason  string
	method  string // for requests
	headers map[string][]string
	body    string
}

func (m *sipMsg) get(name string) string {
	if v := m.headers[strings.ToLower(name)]; len(v) > 0 {
		return v[0]
	}
	return ""
}

func (d *dialog) request(method, target string, extra []string, body string, branch string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s SIP/2.0\r\n", method, target)
	fmt.Fprintf(&b, "Via: SIP/2.0/UDP %s:%d;branch=%s;rport\r\n", d.localIP, d.sipPort, branch)
	b.WriteString("Max-Forwards: 70\r\n")
	fmt.Fprintf(&b, "From: \"%s\" <sip:%s@%s>;tag=%s\r\n", d.from, d.from, d.localIP, d.fromTag)
	to := fmt.Sprintf("<%s>", d.uri)
	if d.toTag != "" {
		to += ";tag=" + d.toTag
	}
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Call-ID: %s\r\n", d.callID)
	fmt.Fprintf(&b, "CSeq: %d %s\r\n", d.cseq, method)
	fmt.Fprintf(&b, "Contact: <sip:%s@%s:%d>\r\n", d.from, d.localIP, d.sipPort)
	b.WriteString("User-Agent: crosswire\r\n")
	if method == "INVITE" && d.session != "" {
		fmt.Fprintf(&b, "%s: %s\r\n", SessionHeader, headerSafe(d.session))
	}
	for _, h := range extra {
		b.WriteString(h + "\r\n")
	}
	if body != "" {
		b.WriteString("Content-Type: application/sdp\r\n")
	}
	fmt.Fprintf(&b, "Content-Length: %d\r\n\r\n%s", len(body), body)
	return b.String()
}

// invite sends INVITE with T1 retransmission until a provisional or final
// response, answers one digest challenge, and ACKs the final response.
func (d *dialog) invite(ctx context.Context, sdp string, cfg model.SIPConfig) (*sipMsg, error) {
	var auth []string
	for attempt := 0; attempt < 2; attempt++ {
		branch := "z9hG4bK" + randHex(8)
		d.lastInviteCSeq, d.lastInviteBranch = d.cseq, branch
		msg := d.request("INVITE", d.uri, auth, sdp, branch)
		resp, err := d.transact(ctx, msg, branch, true)
		if err != nil {
			return nil, err
		}
		d.logf("INVITE -> %d %s", resp.status, resp.reason)
		if tag := paramOf(resp.get("To"), "tag"); tag != "" {
			d.toTag = tag
		}
		if resp.status/100 == 2 {
			d.remoteTarget = contactURI(resp.get("Contact"))
			if d.remoteTarget == "" {
				d.remoteTarget = d.uri
			}
			d.send(d.request("ACK", d.remoteTarget, nil, "", "z9hG4bK"+randHex(8)))
			return resp, nil
		}
		// Non-2xx final responses are ACKed in the INVITE transaction.
		d.send(d.request("ACK", d.uri, nil, "", branch))
		if (resp.status == 401 || resp.status == 407) && attempt == 0 {
			hdr, name := resp.get("WWW-Authenticate"), "Authorization"
			if resp.status == 407 {
				hdr, name = resp.get("Proxy-Authenticate"), "Proxy-Authorization"
			}
			line, err := digestResponse(hdr, "INVITE", d.uri, cfg)
			if err != nil {
				return resp, err
			}
			auth = []string{name + ": " + line}
			d.cseq++
			d.toTag = ""
			continue
		}
		return resp, nil
	}
	return nil, errors.New("authentication rejected")
}

func (d *dialog) send(msg string) {
	_, _ = d.conn.WriteToUDP([]byte(msg), d.remote)
}

// transact sends a request, retransmitting on T1 doubling until a response
// arrives; for INVITE it keeps waiting after provisionals for the final.
func (d *dialog) transact(ctx context.Context, msg, branch string, isInvite bool) (*sipMsg, error) {
	interval := sipT1
	d.send(msg)
	next := time.Now().Add(interval)
	provisional := false
	for {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("no final response: %w", ctx.Err())
		}
		wait := time.Until(next)
		if provisional || wait > time.Second {
			wait = time.Second
		}
		_ = d.conn.SetReadDeadline(time.Now().Add(wait))
		buf := make([]byte, 65535)
		n, addr, err := d.conn.ReadFromUDP(buf)
		if err != nil {
			if !provisional && time.Now().After(next) {
				d.send(msg)
				if interval < 4*time.Second {
					interval *= 2
				}
				next = time.Now().Add(interval)
			}
			continue
		}
		m, perr := parseSIP(string(buf[:n]))
		if perr != nil {
			continue
		}
		if m.method != "" {
			d.handleRequest(m, addr)
			continue
		}
		if !strings.Contains(m.get("Via"), branch) {
			continue
		}
		if m.status < 200 {
			provisional = true
			d.logf("INVITE <- %d %s", m.status, m.reason)
			continue
		}
		return m, nil
	}
}

// handleRequest answers in-dialog requests from the far end (BYE, OPTIONS).
func (d *dialog) handleRequest(m *sipMsg, addr *net.UDPAddr) bool {
	via := m.get("Via")
	reply := func(code int, text string) {
		var b strings.Builder
		fmt.Fprintf(&b, "SIP/2.0 %d %s\r\n", code, text)
		fmt.Fprintf(&b, "Via: %s\r\nFrom: %s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
			via, m.get("From"), m.get("To"), m.get("Call-ID"), m.get("CSeq"))
		_, _ = d.conn.WriteToUDP([]byte(b.String()), addr)
	}
	switch m.method {
	case "BYE":
		reply(200, "OK")
		d.logf("far end sent BYE")
		if msg := m.get(ErrorHeader); msg != "" {
			d.call.Error = msg
		}
		return true
	case "OPTIONS", "INFO", "NOTIFY", "UPDATE":
		reply(200, "OK")
	default:
		reply(405, "Method Not Allowed")
	}
	return false
}

func (d *dialog) serveDuringCall(ctx context.Context, bye chan<- struct{}) {
	buf := make([]byte, 65535)
	for ctx.Err() == nil {
		_ = d.conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, addr, err := d.conn.ReadFromUDP(buf)
		if err != nil {
			continue
		}
		m, perr := parseSIP(string(buf[:n]))
		if perr != nil || m.method == "" {
			continue
		}
		if d.handleRequest(m, addr) {
			select {
			case bye <- struct{}{}:
			default:
			}
			return
		}
	}
}

func (d *dialog) bye(ctx context.Context) error {
	d.cseq++
	branch := "z9hG4bK" + randHex(8)
	target := d.remoteTarget
	if target == "" {
		target = d.uri
	}
	bctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	resp, err := d.transact(bctx, d.request("BYE", target, nil, "", branch), branch, false)
	if err != nil {
		return err
	}
	d.logf("BYE -> %d %s", resp.status, resp.reason)
	return nil
}

func parseSIP(raw string) (*sipMsg, error) {
	head, body, _ := strings.Cut(raw, "\r\n\r\n")
	sc := bufio.NewScanner(strings.NewReader(head))
	if !sc.Scan() {
		return nil, errors.New("empty")
	}
	m := &sipMsg{headers: map[string][]string{}, body: body}
	first := sc.Text()
	if strings.HasPrefix(first, "SIP/2.0 ") {
		parts := strings.SplitN(first, " ", 3)
		if len(parts) < 2 {
			return nil, errors.New("bad status line")
		}
		m.status, _ = strconv.Atoi(parts[1])
		if len(parts) == 3 {
			m.reason = parts[2]
		}
	} else {
		parts := strings.SplitN(first, " ", 3)
		if len(parts) != 3 || parts[2] != "SIP/2.0" {
			return nil, errors.New("bad request line")
		}
		m.method = parts[0]
	}
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		k = strings.ToLower(strings.TrimSpace(k))
		switch k {
		case "v":
			k = "via"
		case "f":
			k = "from"
		case "t":
			k = "to"
		case "i":
			k = "call-id"
		case "m":
			k = "contact"
		case "c":
			k = "content-type"
		case "l":
			k = "content-length"
		}
		m.headers[k] = append(m.headers[k], strings.TrimSpace(v))
	}
	return m, nil
}

// headerSafe keeps a value on one header line.
func headerSafe(v string) string {
	v = strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' {
			return ' '
		}
		return r
	}, v)
	return strings.TrimSpace(v)
}

func parseSIPURI(uri string) (host, port, user string, err error) {
	s := strings.TrimPrefix(uri, "sip:")
	if s == uri {
		return "", "", "", errors.New("endpoint must be a sip: URI")
	}
	if i := strings.IndexAny(s, ";?"); i >= 0 {
		s = s[:i]
	}
	if u, h, ok := strings.Cut(s, "@"); ok {
		user, s = u, h
	}
	host, port, err = net.SplitHostPort(s)
	if err != nil {
		host, port = s, "5060"
	}
	if host == "" {
		return "", "", "", errors.New("sip URI has no host")
	}
	return host, port, user, nil
}

func parseSDP(body string) (*net.UDPAddr, error) {
	var ip, port string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "c=IN IP4 "):
			ip = strings.TrimPrefix(line, "c=IN IP4 ")
		case strings.HasPrefix(line, "m=audio "):
			f := strings.Fields(line)
			if len(f) > 1 {
				port = f[1]
			}
		}
	}
	if ip == "" || port == "" {
		return nil, errors.New("answer has no audio in its SDP")
	}
	return net.ResolveUDPAddr("udp", net.JoinHostPort(ip, port))
}

func paramOf(header, name string) string {
	for _, p := range strings.Split(header, ";")[1:] {
		if k, v, ok := strings.Cut(strings.TrimSpace(p), "="); ok && k == name {
			return v
		}
	}
	return ""
}

func contactURI(h string) string {
	if i := strings.Index(h, "<"); i >= 0 {
		if j := strings.Index(h[i:], ">"); j > 0 {
			return h[i+1 : i+j]
		}
	}
	return strings.TrimSpace(strings.Split(h, ";")[0])
}

// digestResponse builds the RFC 2617 Digest credentials for a challenge.
// The password comes from the environment variable the target names.
func digestResponse(challenge, method, uri string, cfg model.SIPConfig) (string, error) {
	if !strings.HasPrefix(strings.ToLower(challenge), "digest ") {
		return "", fmt.Errorf("unsupported challenge %q", challenge)
	}
	params := map[string]string{}
	for _, p := range splitParams(challenge[len("Digest "):]) {
		k, v, _ := strings.Cut(p, "=")
		params[strings.ToLower(strings.TrimSpace(k))] = strings.Trim(strings.TrimSpace(v), `"`)
	}
	if cfg.Username == "" || cfg.PasswordEnv == "" {
		return "", errors.New("target asked for authentication but no sip username/password_env is configured")
	}
	pass := os.Getenv(cfg.PasswordEnv)
	if pass == "" {
		return "", fmt.Errorf("environment variable %s is not set", cfg.PasswordEnv)
	}
	h := func(s string) string { sum := md5.Sum([]byte(s)); return hex.EncodeToString(sum[:]) }
	ha1 := h(cfg.Username + ":" + params["realm"] + ":" + pass)
	ha2 := h(method + ":" + uri)
	var resp string
	qop := ""
	for _, q := range strings.Split(params["qop"], ",") {
		if strings.TrimSpace(q) == "auth" {
			qop = "auth"
		}
	}
	cnonce, nc := randHex(8), "00000001"
	if qop == "auth" {
		resp = h(ha1 + ":" + params["nonce"] + ":" + nc + ":" + cnonce + ":auth:" + ha2)
	} else {
		resp = h(ha1 + ":" + params["nonce"] + ":" + ha2)
	}
	out := fmt.Sprintf(`Digest username="%s", realm="%s", nonce="%s", uri="%s", response="%s", algorithm=MD5`,
		cfg.Username, params["realm"], params["nonce"], uri, resp)
	if qop == "auth" {
		out += fmt.Sprintf(`, qop=auth, nc=%s, cnonce="%s"`, nc, cnonce)
	}
	if op := params["opaque"]; op != "" {
		out += fmt.Sprintf(`, opaque="%s"`, op)
	}
	return out, nil
}

func splitParams(s string) []string {
	var out []string
	inQuote := false
	start := 0
	for i, c := range s {
		switch {
		case c == '"':
			inQuote = !inQuote
		case c == ',' && !inQuote:
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

func listenRTP(ip string) (*net.UDPConn, error) {
	// RTP conventionally sits on an even port; try a few times.
	for i := 0; i < 8; i++ {
		c, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP(ip)})
		if err != nil {
			return nil, fmt.Errorf("rtp socket: %w", err)
		}
		if c.LocalAddr().(*net.UDPAddr).Port%2 == 0 {
			return c, nil
		}
		c.Close()
	}
	return net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP(ip)})
}

func sendRTP(ctx context.Context, conn *net.UDPConn, to *net.UDPAddr, ulaw []byte, call *Call) {
	ssrc := randBytes(4)
	seq := uint16(randBytes(2)[0])<<8 | uint16(randBytes(2)[1])
	ts := uint32(0)
	tick := time.NewTicker(rtpInterval)
	defer tick.Stop()
	pkt := make([]byte, 12+rtpFrame)
	for off := 0; off < len(ulaw); off += rtpFrame {
		if ctx.Err() != nil {
			return
		}
		end := off + rtpFrame
		if end > len(ulaw) {
			end = len(ulaw)
		}
		pkt[0] = 0x80 // V=2
		pkt[1] = 0    // PCMU, no marker
		if off == 0 {
			pkt[1] = 0x80 // marker on the first packet
		}
		pkt[2], pkt[3] = byte(seq>>8), byte(seq)
		pkt[4], pkt[5], pkt[6], pkt[7] = byte(ts>>24), byte(ts>>16), byte(ts>>8), byte(ts)
		copy(pkt[8:12], ssrc)
		n := copy(pkt[12:], ulaw[off:end])
		// Pad the last frame with silence so every packet is 20 ms.
		for i := 12 + n; i < len(pkt); i++ {
			pkt[i] = 0xFF
		}
		if _, err := conn.WriteToUDP(pkt, to); err == nil {
			call.SentPackets++
		}
		seq++
		ts += rtpFrame
		<-tick.C
	}
	call.SentSeconds = float64(call.SentPackets) * rtpInterval.Seconds()
}

func receiveRTP(ctx context.Context, conn *net.UDPConn, call *Call) []byte {
	var out []byte
	buf := make([]byte, 2048)
	for ctx.Err() == nil {
		_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil || n < 12 || buf[0]>>6 != 2 {
			continue
		}
		hdr := 12 + 4*int(buf[0]&0x0F)
		if buf[0]&0x10 != 0 && n >= hdr+4 { // extension
			hdr += 4 + 4*int(uint16(buf[hdr+2])<<8|uint16(buf[hdr+3]))
		}
		if buf[1]&0x7F != 0 || n <= hdr {
			continue // not PCMU
		}
		call.ReceivedPackets++
		if len(out) < 8000*maxCallSecs {
			out = append(out, buf[hdr:n]...)
		}
	}
	return out
}

func outboundIP(remote *net.UDPAddr) (string, error) {
	c, err := net.DialUDP("udp", nil, remote)
	if err != nil {
		return "", fmt.Errorf("route to %s: %w", remote, err)
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).IP.String(), nil
}

func randBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

func randHex(n int) string { return hex.EncodeToString(randBytes(n)) }
