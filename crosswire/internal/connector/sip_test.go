package connector

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/model"
	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/signal"
)

// fakeUAS is a minimal SIP answering endpoint on loopback: it rings, optionally
// challenges for digest credentials, answers with SDP, counts the RTP it
// receives, plays a short tone back, and acknowledges BYE.
type fakeUAS struct {
	t         *testing.T
	sip, rtp  *net.UDPConn
	challenge bool
	hangUp    bool // send BYE after the tone instead of waiting for the caller's

	mu        sync.Mutex
	rtpIn     int
	authSeen  string
	methods   []string
	byeAcked  bool
	callerRTP *net.UDPAddr
}

func newUAS(t *testing.T, challenge, hangUp bool) *fakeUAS {
	t.Helper()
	sip, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	rtp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	u := &fakeUAS{t: t, sip: sip, rtp: rtp, challenge: challenge, hangUp: hangUp}
	go u.serve()
	go u.media()
	t.Cleanup(func() { sip.Close(); rtp.Close() })
	return u
}

func (u *fakeUAS) uri() string {
	return fmt.Sprintf("sip:agent@127.0.0.1:%d", u.sip.LocalAddr().(*net.UDPAddr).Port)
}

func (u *fakeUAS) reply(to *net.UDPAddr, req *sipMsg, code int, text string, extra []string, body string) {
	var b strings.Builder
	fmt.Fprintf(&b, "SIP/2.0 %d %s\r\n", code, text)
	toHdr := req.get("To")
	if code != 100 && !strings.Contains(toHdr, "tag=") {
		toHdr += ";tag=uas1"
	}
	fmt.Fprintf(&b, "Via: %s\r\nFrom: %s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: %s\r\n", req.get("Via"), req.get("From"), toHdr, req.get("Call-ID"), req.get("CSeq"))
	for _, e := range extra {
		b.WriteString(e + "\r\n")
	}
	if body != "" {
		b.WriteString("Content-Type: application/sdp\r\n")
	}
	fmt.Fprintf(&b, "Content-Length: %d\r\n\r\n%s", len(body), body)
	_, _ = u.sip.WriteToUDP([]byte(b.String()), to)
}

func (u *fakeUAS) serve() {
	buf := make([]byte, 65535)
	challenged := false
	for {
		n, from, err := u.sip.ReadFromUDP(buf)
		if err != nil {
			return
		}
		m, err := parseSIP(string(buf[:n]))
		if err != nil || m.method == "" {
			continue
		}
		u.mu.Lock()
		u.methods = append(u.methods, m.method)
		u.mu.Unlock()
		switch m.method {
		case "INVITE":
			if u.challenge && !challenged {
				challenged = true
				u.reply(from, m, 401, "Unauthorized", []string{`WWW-Authenticate: Digest realm="lab", nonce="n1", qop="auth", algorithm=MD5`}, "")
				continue
			}
			u.mu.Lock()
			u.authSeen = m.get("Authorization")
			u.mu.Unlock()
			rtpAddr, err := parseSDP(m.body)
			if err != nil {
				u.reply(from, m, 488, "Not Acceptable Here", nil, "")
				continue
			}
			u.mu.Lock()
			u.callerRTP = rtpAddr
			u.mu.Unlock()
			u.reply(from, m, 100, "Trying", nil, "")
			u.reply(from, m, 180, "Ringing", nil, "")
			port := u.rtp.LocalAddr().(*net.UDPAddr).Port
			sdp := fmt.Sprintf("v=0\r\no=- 1 1 IN IP4 127.0.0.1\r\ns=uas\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\nm=audio %d RTP/AVP 0\r\na=rtpmap:0 PCMU/8000\r\n", port)
			u.reply(from, m, 200, "OK", []string{fmt.Sprintf("Contact: <sip:agent@127.0.0.1:%d>", u.sip.LocalAddr().(*net.UDPAddr).Port)}, sdp)
		case "ACK":
			if u.hangUp {
				go u.sendBye(from, m)
			}
		case "BYE":
			u.reply(from, m, 200, "OK", nil, "")
			u.mu.Lock()
			u.byeAcked = true
			u.mu.Unlock()
		default:
			u.reply(from, m, 200, "OK", nil, "")
		}
	}
}

func (u *fakeUAS) sendBye(to *net.UDPAddr, ack *sipMsg) {
	time.Sleep(1200 * time.Millisecond) // after the caller has played
	var b strings.Builder
	fmt.Fprintf(&b, "BYE %s SIP/2.0\r\n", contactURI(ack.get("Contact")))
	fmt.Fprintf(&b, "Via: SIP/2.0/UDP 127.0.0.1:%d;branch=z9hG4bKuas\r\n", u.sip.LocalAddr().(*net.UDPAddr).Port)
	fmt.Fprintf(&b, "From: %s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: 1 BYE\r\nContent-Length: 0\r\n\r\n", ack.get("To"), ack.get("From"), ack.get("Call-ID"))
	_, _ = u.sip.WriteToUDP([]byte(b.String()), to)
}

func (u *fakeUAS) media() {
	buf := make([]byte, 2048)
	played := false
	for {
		n, from, err := u.rtp.ReadFromUDP(buf)
		if err != nil {
			return
		}
		if n < 12 {
			continue
		}
		u.mu.Lock()
		u.rtpIn++
		first := !played
		played = true
		u.mu.Unlock()
		if first {
			go u.playTone(from)
		}
	}
}

// playTone sends 0.5 s of PCMU back to whoever sent RTP first.
func (u *fakeUAS) playTone(to *net.UDPAddr) {
	pcm := signal.Tone(8000, 4000, 0.5, 600)
	pkt := make([]byte, 12+160)
	for i := 0; i < len(pcm); i += 160 {
		pkt[0] = 0x80
		pkt[2], pkt[3] = byte(i/160>>8), byte(i/160)
		for j := 0; j < 160 && i+j < len(pcm); j++ {
			pkt[12+j] = signal.MuLawEncode(pcm[i+j])
		}
		_, _ = u.rtp.WriteToUDP(pkt, to)
		time.Sleep(20 * time.Millisecond)
	}
}

func testWAV() []byte { return signal.WAV(16000, signal.Tone(16000, 16000, 0.5, 440)) } // 1 s

func TestDialPlaysAndRecordsAndHangsUp(t *testing.T) {
	u := newUAS(t, false, false)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	call, err := Dial(ctx, model.SIPConfig{LocalIP: "127.0.0.1", ListenSeconds: 1}, u.uri(), testWAV())
	if err != nil {
		t.Fatalf("%v\n%s", err, strings.Join(call.Log, "\n"))
	}
	if !call.Answered || call.Status != 200 || call.SentPackets != 50 || call.SentSeconds != 1 {
		t.Fatalf("%+v", call)
	}
	if call.ReceivedPackets < 20 || call.ReceivedSeconds < 0.4 || len(call.ReplyAudio) == 0 {
		t.Fatalf("far end audio not recorded: %+v", call)
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.rtpIn != 50 || !u.byeAcked {
		t.Fatalf("uas saw %d rtp packets, bye acked %v, methods %v", u.rtpIn, u.byeAcked, u.methods)
	}
	if strings.Join(u.methods, ",") != "INVITE,ACK,BYE" {
		t.Fatalf("methods %v", u.methods)
	}
}

func TestDialAnswersDigestChallenge(t *testing.T) {
	u := newUAS(t, true, false)
	t.Setenv("TEST_SIP_PASS", "s3cret")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cfg := model.SIPConfig{LocalIP: "127.0.0.1", ListenSeconds: 1, Username: "alice", PasswordEnv: "TEST_SIP_PASS"}
	call, err := Dial(ctx, cfg, u.uri(), testWAV())
	if err != nil {
		t.Fatalf("%v\n%s", err, strings.Join(call.Log, "\n"))
	}
	u.mu.Lock()
	auth := u.authSeen
	methods := strings.Join(u.methods, ",")
	u.mu.Unlock()
	if !strings.Contains(auth, `username="alice"`) || !strings.Contains(auth, `realm="lab"`) || !strings.Contains(auth, "qop=auth") {
		t.Fatalf("authorization %q", auth)
	}
	// Known-answer: HA1=md5(alice:lab:s3cret), HA2=md5(INVITE:uri), nc=00000001, cnonce varies.
	if !strings.Contains(auth, `response="`) {
		t.Fatalf("no response in %q", auth)
	}
	if methods != "INVITE,ACK,INVITE,ACK,BYE" {
		t.Fatalf("methods %s", methods)
	}
	if _, err := Dial(ctx, model.SIPConfig{LocalIP: "127.0.0.1"}, newUAS(t, true, false).uri(), testWAV()); err == nil || !strings.Contains(err.Error(), "no sip username") {
		t.Fatalf("challenge without credentials must fail clearly, got %v", err)
	}
}

func TestDialHandlesFarEndHangUp(t *testing.T) {
	u := newUAS(t, false, true)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	call, err := Dial(ctx, model.SIPConfig{LocalIP: "127.0.0.1", ListenSeconds: 10}, u.uri(), testWAV())
	if err != nil {
		t.Fatalf("%v\n%s", err, strings.Join(call.Log, "\n"))
	}
	if !call.RemoteHungUp {
		t.Fatalf("expected far-end BYE: %+v", call)
	}
}

func TestSendRoutesSIPTargets(t *testing.T) {
	u := newUAS(t, false, false)
	tgt := model.Target{Transport: "sip", Endpoint: u.uri(), SIP: model.SIPConfig{LocalIP: "127.0.0.1", ListenSeconds: 1}}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := Send(ctx, tgt, Perception, Input{Content: "x"}); err == nil || !strings.Contains(err.Error(), "needs an audio artifact") {
		t.Fatalf("text through sip must fail: %v", err)
	}
	ex, err := Send(ctx, tgt, Perception, Input{Content: "hello", AudioBase64: base64.StdEncoding.EncodeToString(testWAV())})
	if err != nil {
		t.Fatalf("%v %s", err, ex.RawResponse)
	}
	if ex.Status != 200 || !strings.Contains(ex.ResponseText, "call answered") || len(ex.ReplyAudio) == 0 || ex.Request["transport"] != "sip" {
		t.Fatalf("%+v", ex)
	}
	if _, err := Dial(ctx, model.SIPConfig{LocalIP: "127.0.0.1", RingSeconds: 1}, "sip:nobody@127.0.0.1:1", testWAV()); err == nil {
		t.Fatal("unanswered call must fail")
	}
}
