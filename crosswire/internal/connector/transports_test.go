package connector

import (
	"context"
	"encoding/base64"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/fakes"
	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/model"
)

// recorder collects the sink events a fake agent reports.
type recorder struct {
	mu  sync.Mutex
	evs []model.SinkEvent
}

func (r *recorder) report(ev model.SinkEvent) { r.mu.Lock(); r.evs = append(r.evs, ev); r.mu.Unlock() }
func (r *recorder) events() []model.SinkEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]model.SinkEvent(nil), r.evs...)
}

func newAgent() (*fakes.Agent, *recorder) {
	r := &recorder{}
	return &fakes.Agent{Name: "fake", Report: r.report}, r
}

const canary = "cw-0a1b2c3d"

func inputFor(text string) Input {
	return Input{Source: "chat/text", Content: text, Canary: canary}
}

func send(t *testing.T, tg model.Target, arm Arm, in Input) (model.Exchange, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return Send(ctx, tg, arm, in)
}

func TestWebSocketRoundTrip(t *testing.T) {
	a, rec := newAgent()
	f := fakes.NewWS(a)
	defer f.Close()
	t.Setenv("WS_TEST_TOKEN", "s3cret")
	tg := model.Target{Transport: "ws", Endpoint: f.URL(), RequestTemplate: `{"input":"{{content}}","session":"{{canary}}"}`,
		Headers: map[string]string{"X-Trace": "abc"}, SecretHeaders: map[string]string{"X-Token": "WS_TEST_TOKEN"},
		WS: model.WSConfig{Subprotocol: "agent.v1"}, TimeoutSeconds: 5}

	ex, err := send(t, tg, Perception, inputFor("hello "+fakes.ObeyMarker+" "+canary))
	if err != nil {
		t.Fatal(err)
	}
	if ex.ResponseText != "done" || len(ex.Actions) != 1 || ex.Actions[0].Name != "send_email" {
		t.Fatalf("reply not parsed: %+v", ex)
	}
	if !strings.Contains(ex.Perceived, canary) {
		t.Errorf("perceived should echo the canary: %q", ex.Perceived)
	}
	if evs := rec.events(); len(evs) != 1 || evs[0].Session != canary {
		t.Errorf("sink report: %+v", evs)
	}
	if f.Header().Get("X-Trace") != "abc" || f.Header().Get("X-Token") != "s3cret" {
		t.Errorf("headers not sent: %v", f.Header())
	}
	if f.Subprotocol() != "agent.v1" {
		t.Errorf("subprotocol: %q", f.Subprotocol())
	}
	if ex.Wire["transport"] != "ws" {
		t.Errorf("wire: %v", ex.Wire)
	}
	if got := f.Received(); len(got) != 1 || !strings.Contains(got[0], `"session":"`+canary+`"`) {
		t.Errorf("request template not rendered: %v", got)
	}

	// A control without the marker does nothing.
	ex, err = send(t, tg, Perception, inputFor("shelf B4 restocked "+canary))
	if err != nil || len(ex.Actions) != 0 || len(rec.events()) != 1 {
		t.Errorf("control: %v %+v", err, ex)
	}
}

func TestWebSocketDirectArmUsesEndpointScheme(t *testing.T) {
	a, _ := newAgent()
	f := fakes.NewWS(a)
	defer f.Close()
	tg := model.Target{Endpoint: "http://127.0.0.1:1/unused", DirectEndpoint: f.URL(), DirectTemplate: `{"message":"{{content}}"}`, TimeoutSeconds: 5}
	ex, err := send(t, tg, Direct, inputFor("hi"))
	if err != nil || ex.Wire["transport"] != "ws" {
		t.Fatalf("direct over ws: %v %+v", err, ex)
	}
}

func TestWebSocketErrors(t *testing.T) {
	tg := model.Target{Transport: "ws", Endpoint: "http://127.0.0.1:1/x", TimeoutSeconds: 2}
	if _, err := send(t, tg, Perception, inputFor("x")); err == nil {
		t.Error("an http URL is not a websocket endpoint")
	}
	tg.Endpoint = "ws://127.0.0.1:1/x"
	if ex, err := send(t, tg, Perception, inputFor("x")); err == nil || ex.Error == "" {
		t.Error("connection refused must be an error with ex.Error set")
	}
	tg.Endpoint = "ws://169.254.169.254/x"
	if _, err := send(t, tg, Perception, inputFor("x")); err == nil || !strings.Contains(err.Error(), "link-local") {
		t.Errorf("metadata address must be refused: %v", err)
	}
	tg.Endpoint = "ws://127.0.0.1:1/x"
	tg.SecretHeaders = map[string]string{"X-Token": "WS_TOKEN_NOT_SET_ANYWHERE"}
	if _, err := send(t, tg, Perception, inputFor("x")); err == nil || !strings.Contains(err.Error(), "WS_TOKEN_NOT_SET_ANYWHERE") {
		t.Errorf("unset secret header must name the variable: %v", err)
	}
}

func TestWebSocketReplyTimeout(t *testing.T) {
	// An agent that reads and never answers: use a voice fake that stalls.
	a, _ := newAgent()
	v := fakes.NewVoiceWS(a)
	v.Stall = true
	defer v.Close()
	tg := model.Target{Transport: "ws", Endpoint: v.URL(), TimeoutSeconds: 1}
	start := time.Now()
	_, err := send(t, tg, Perception, inputFor("x"))
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("want timeout, got %v", err)
	}
	if time.Since(start) > 4*time.Second {
		t.Errorf("timeout took %v", time.Since(start))
	}
}

func TestWebSocketTLS(t *testing.T) {
	srv, cli := fakes.SelfSignedTLS()
	a, _ := newAgent()
	f := fakes.NewWSTLS(a, srv)
	defer f.Close()
	tg := model.Target{Transport: "ws", Endpoint: f.URL(), TimeoutSeconds: 3}
	if !strings.HasPrefix(f.URL(), "wss://") {
		t.Fatalf("want wss, got %s", f.URL())
	}
	testTLS = cli
	defer func() { testTLS = nil }()
	if ex, err := send(t, tg, Perception, inputFor("x")); err != nil || ex.ResponseText != "ok" {
		t.Fatalf("wss with a trusted certificate: %v %+v", err, ex)
	}
	testTLS = nil
	if _, err := send(t, tg, Perception, inputFor("x")); err == nil {
		t.Error("an untrusted certificate must be refused")
	}
}

func tcpTarget(url string, cfg model.TCPConfig) model.Target {
	return model.Target{Transport: "tcp", Endpoint: url, RequestTemplate: `{"input":"{{content}}"}`, TCP: cfg, TimeoutSeconds: 3}
}

func TestTCPLineProtocol(t *testing.T) {
	a, rec := newAgent()
	f := fakes.NewTCP(a, fakes.TCPOptions{})
	defer f.Close()
	ex, err := send(t, tcpTarget(f.URL(), model.TCPConfig{}), Perception, inputFor(fakes.ObeyMarker+" "+canary))
	if err != nil {
		t.Fatal(err)
	}
	if ex.ResponseText != "done" || len(ex.Actions) != 1 {
		t.Fatalf("reply: %+v", ex)
	}
	if got := f.Received(); len(got) != 1 || !strings.HasPrefix(got[0], `{"input":`) {
		t.Errorf("json send: %v", got)
	}
	if len(rec.events()) != 1 {
		t.Error("sink event missing")
	}
}

func TestTCPRawCustomTerminatorAndLength(t *testing.T) {
	a, _ := newAgent()
	crlf := fakes.NewTCP(a, fakes.TCPOptions{Terminator: "\r\n"})
	defer crlf.Close()
	ex, err := send(t, tcpTarget(crlf.URL(), model.TCPConfig{Send: "raw", Terminator: `\r\n`}), Perception, inputFor("plain text "+canary))
	if err != nil {
		t.Fatal(err)
	}
	if got := crlf.Received(); len(got) != 1 || got[0] != "plain text "+canary {
		t.Errorf("raw send: %v", got)
	}
	if ex.ResponseText != "ok" {
		t.Errorf("reply: %q", ex.ResponseText)
	}

	lf := fakes.NewTCP(a, fakes.TCPOptions{Length: true})
	defer lf.Close()
	ex, err = send(t, tcpTarget(lf.URL(), model.TCPConfig{Framing: "length"}), Perception, inputFor("framed"))
	if err != nil || ex.ResponseText != "ok" {
		t.Fatalf("length framing: %v %+v", err, ex)
	}
}

func TestTCPReadModes(t *testing.T) {
	a, _ := newAgent()
	noTerm := fakes.NewTCP(a, fakes.TCPOptions{NoTerm: true})
	defer noTerm.Close()
	start := time.Now()
	ex, err := send(t, tcpTarget(noTerm.URL(), model.TCPConfig{ReadUntil: "idle", IdleMs: 200}), Perception, inputFor("x"))
	if err != nil || ex.ResponseText != "ok" {
		t.Fatalf("idle read: %v %+v", err, ex)
	}
	if time.Since(start) > 1500*time.Millisecond {
		t.Errorf("idle read waited for the close: %v", time.Since(start))
	}

	// A reply with no terminator and no close is returned at the timeout.
	tg := tcpTarget(noTerm.URL(), model.TCPConfig{})
	tg.TimeoutSeconds = 1
	ex, err = send(t, tg, Perception, inputFor("x"))
	if err != nil || ex.ResponseText != "ok" {
		t.Fatalf("timeout with data: %v %+v", err, ex)
	}

	closing := fakes.NewTCP(a, fakes.TCPOptions{})
	defer closing.Close()
	ex, err = send(t, tcpTarget(closing.URL(), model.TCPConfig{ReadUntil: "close"}), Perception, inputFor("x"))
	if err != nil || ex.ResponseText != "ok" {
		t.Fatalf("close read: %v %+v", err, ex)
	}

	silent := fakes.NewTCP(a, fakes.TCPOptions{Silent: true})
	defer silent.Close()
	tg = tcpTarget(silent.URL(), model.TCPConfig{})
	tg.TimeoutSeconds = 1
	if _, err = send(t, tg, Perception, inputFor("x")); err == nil {
		t.Error("no reply at all must be an error")
	}
	ex, err = send(t, tcpTarget(silent.URL(), model.TCPConfig{ReadUntil: "none"}), Perception, inputFor("x"))
	if err != nil || !strings.Contains(ex.ResponseText, "no reply is read") {
		t.Errorf("read_until none: %v %+v", err, ex)
	}
}

func TestTCPLimitsAndGuards(t *testing.T) {
	a, _ := newAgent()
	f := fakes.NewTCP(a, fakes.TCPOptions{})
	defer f.Close()
	if _, err := send(t, tcpTarget(f.URL(), model.TCPConfig{MaxBytes: 2}), Perception, inputFor("x")); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("reply cap: %v", err)
	}
	if _, err := send(t, tcpTarget("tcp://8.8.8.8:80", model.TCPConfig{}), Perception, inputFor("x")); err == nil || !strings.Contains(err.Error(), "private") {
		t.Errorf("public raw TCP must be refused: %v", err)
	}
	if _, err := send(t, tcpTarget("http://127.0.0.1:1", model.TCPConfig{}), Perception, inputFor("x")); err == nil {
		t.Error("tcp endpoint must be tcp://")
	}
	if _, err := send(t, tcpTarget(f.URL(), model.TCPConfig{Framing: "weird"}), Perception, inputFor("x")); err == nil {
		t.Error("unknown framing must be refused")
	}
}

func TestTCPOverTLS(t *testing.T) {
	srv, cli := fakes.SelfSignedTLS()
	testTLS = cli
	defer func() { testTLS = nil }()
	a, _ := newAgent()
	f := fakes.NewTCP(a, fakes.TCPOptions{TLS: srv})
	defer f.Close()
	ex, err := send(t, tcpTarget(f.URL(), model.TCPConfig{TLS: true}), Perception, inputFor("x"))
	if err != nil || ex.ResponseText != "ok" {
		t.Fatalf("tls: %v %+v", err, ex)
	}
	testTLS = nil
	if _, err := send(t, tcpTarget(f.URL(), model.TCPConfig{TLS: true}), Perception, inputFor("x")); err == nil {
		t.Error("an untrusted certificate must be refused")
	}
}

func TestParseTerminator(t *testing.T) {
	tests := []struct {
		in   string
		want string
		bad  bool
	}{
		{"", "\n", false}, {`\n`, "\n", false}, {`\r\n`, "\r\n", false}, {`\0`, "\x00", false},
		{`\x0a`, "\n", false}, {"none", "", false}, {`;`, ";", false}, {`\q`, "", true}, {`\x0`, "", true}, {`\`, "", true},
	}
	for _, tc := range tests {
		got, err := ParseTerminator(tc.in)
		if (err != nil) != tc.bad || (!tc.bad && string(got) != tc.want) {
			t.Errorf("ParseTerminator(%q) = %q, %v", tc.in, got, err)
		}
	}
}

func mqttTarget(b *fakes.MQTTBroker, cfg model.MQTTConfig) model.Target {
	if cfg.PublishTopic == "" {
		cfg.PublishTopic = "agents/in"
	}
	if cfg.ResponseTopic == "" {
		cfg.ResponseTopic = "agents/out"
	}
	return model.Target{Transport: "mqtt", Endpoint: b.URL(), RequestTemplate: `{"input":"{{content}}","correlation_id":"{{canary}}"}`, MQTT: cfg, TimeoutSeconds: 3}
}

func TestMQTTNonceCorrelation(t *testing.T) {
	b := fakes.NewMQTTBroker()
	defer b.Close()
	a, rec := newAgent()
	fakes.NewMQTTAgent(b, "agents/in", "agents/out", a, fakes.MQTTAgentOptions{Noise: true})
	ex, err := send(t, mqttTarget(b, model.MQTTConfig{}), Perception, inputFor(fakes.ObeyMarker+" "+canary))
	if err != nil {
		t.Fatal(err)
	}
	if ex.ResponseText != "done" || len(ex.Actions) != 1 {
		t.Fatalf("the noise reply must be skipped and the matching one taken: %+v", ex)
	}
	if len(rec.events()) != 1 {
		t.Error("sink event missing")
	}
	pub := b.Published()
	if len(pub) == 0 || pub[0].Topic != "agents/in" || pub[0].QoS != 1 || pub[0].Retain {
		t.Errorf("publish: %+v", pub)
	}
	if !strings.HasPrefix(b.LastClientID(), "cw-") {
		t.Errorf("client id: %q", b.LastClientID())
	}
	if ex.Wire["correlate"] != "nonce" || ex.Wire["qos"] != 1 {
		t.Errorf("wire: %v", ex.Wire)
	}
}

func TestMQTTPathFirstAndQoS0(t *testing.T) {
	b := fakes.NewMQTTBroker()
	defer b.Close()
	a, _ := newAgent()
	// The agent answers with the request echoed in "perceived"; correlation by
	// path finds the canary there.
	fakes.NewMQTTAgent(b, "agents/in", "agents/out", a, fakes.MQTTAgentOptions{Noise: true})
	zero := 0
	ex, err := send(t, mqttTarget(b, model.MQTTConfig{Correlate: "path", CorrelationPath: "perceived", QoS: &zero}), Perception, inputFor("x "+canary))
	if err != nil || !strings.Contains(ex.Perceived, canary) {
		t.Fatalf("path correlation: %v %+v", err, ex)
	}
	if ex.Wire["qos"] != 0 {
		t.Errorf("qos 0 not used: %v", ex.Wire)
	}
	ex, err = send(t, mqttTarget(b, model.MQTTConfig{Correlate: "first"}), Perception, inputFor("x "+canary))
	if err != nil || !strings.Contains(ex.RawResponse, "another session") {
		t.Fatalf("first message: %v %+v", err, ex)
	}
}

func TestMQTTTopicsWithCanary(t *testing.T) {
	b := fakes.NewMQTTBroker()
	defer b.Close()
	a, _ := newAgent()
	fakes.NewMQTTAgent(b, "agents/+/in", "agents/"+canary+"/out", a, fakes.MQTTAgentOptions{})
	tg := mqttTarget(b, model.MQTTConfig{PublishTopic: "agents/{{canary}}/in", ResponseTopic: "agents/{{canary}}/out"})
	if _, err := send(t, tg, Perception, inputFor("x")); err != nil {
		t.Fatal(err)
	}
}

func TestMQTTAuthFromEnvNames(t *testing.T) {
	b := fakes.NewMQTTBroker()
	b.RequireAuth("lab", "hunter2")
	defer b.Close()
	a, _ := newAgent()
	fakes.NewMQTTAgent(b, "agents/in", "agents/out", a, fakes.MQTTAgentOptions{})
	cfg := model.MQTTConfig{UsernameEnv: "MQTT_T_USER", PasswordEnv: "MQTT_T_PASS"}

	if _, err := send(t, mqttTarget(b, cfg), Perception, inputFor("x")); err == nil || !strings.Contains(err.Error(), "MQTT_T_USER") {
		t.Fatalf("unset username variable must be named: %v", err)
	}
	t.Setenv("MQTT_T_USER", "lab")
	t.Setenv("MQTT_T_PASS", "wrong")
	_, err := send(t, mqttTarget(b, cfg), Perception, inputFor("x"))
	if err == nil || !strings.Contains(err.Error(), "bad user name or password") {
		t.Fatalf("wrong password: %v", err)
	}
	if strings.Contains(err.Error(), "wrong") && strings.Contains(err.Error(), "hunter2") {
		t.Error("error must not print a secret")
	}
	t.Setenv("MQTT_T_PASS", "hunter2")
	if _, err := send(t, mqttTarget(b, cfg), Perception, inputFor("x")); err != nil {
		t.Fatalf("right credentials: %v", err)
	}
	if _, err := send(t, mqttTarget(b, model.MQTTConfig{PasswordEnv: "MQTT_T_PASS"}), Perception, inputFor("x")); err == nil {
		t.Error("a password without a username must be refused")
	}
}

func TestMQTTTLS(t *testing.T) {
	srv, cli := fakes.SelfSignedTLS()
	testTLS = cli
	defer func() { testTLS = nil }()
	b := fakes.NewMQTTBrokerTLS(srv)
	defer b.Close()
	a, _ := newAgent()
	fakes.NewMQTTAgent(b, "agents/in", "agents/out", a, fakes.MQTTAgentOptions{})
	if _, err := send(t, mqttTarget(b, model.MQTTConfig{}), Perception, inputFor("x")); err != nil {
		t.Fatalf("mqtts: %v", err)
	}
	testTLS = nil
	if _, err := send(t, mqttTarget(b, model.MQTTConfig{}), Perception, inputFor("x")); err == nil {
		t.Error("an untrusted broker certificate must be refused")
	}
}

func TestMQTTFailures(t *testing.T) {
	b := fakes.NewMQTTBroker()
	defer b.Close()
	tg := mqttTarget(b, model.MQTTConfig{})
	tg.TimeoutSeconds = 1
	if _, err := send(t, tg, Perception, inputFor("x")); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("no agent: %v", err)
	}
	for _, topic := range []string{"agents/#", "agents/+/x", ""} {
		bad := mqttTarget(b, model.MQTTConfig{PublishTopic: topic, ResponseTopic: "agents/out"})
		if topic == "" {
			bad.MQTT.PublishTopic = ""
			bad.MQTT.ResponseTopic = "agents/out"
		}
		if _, err := send(t, bad, Perception, inputFor("x")); err == nil {
			t.Errorf("publish topic %q must be refused", topic)
		}
	}
	two := 2
	if _, err := send(t, mqttTarget(b, model.MQTTConfig{QoS: &two}), Perception, inputFor("x")); err == nil {
		t.Error("qos 2 is not supported")
	}
	pub := model.Target{Transport: "mqtt", Endpoint: "mqtt://8.8.8.8:1883", MQTT: model.MQTTConfig{PublishTopic: "a", ResponseTopic: "b"}, TimeoutSeconds: 1}
	if _, err := send(t, pub, Perception, inputFor("x")); err == nil || !strings.Contains(err.Error(), "private") {
		t.Errorf("public broker must be refused: %v", err)
	}
	if _, err := send(t, model.Target{Transport: "mqtt", Endpoint: "tcp://127.0.0.1:1"}, Perception, inputFor("x")); err == nil {
		t.Error("scheme must be mqtt or mqtts")
	}
}

func voiceTarget(url string) model.Target {
	return model.Target{Transport: "voice-ws", Endpoint: url, RequestTemplate: `{"type":"session.start","input":"{{content}}"}`,
		TimeoutSeconds: 3, VoiceWS: model.VoiceWSConfig{ChunkBytes: 1024, ReplyAudioPath: "audio"}}
}

func voiceInput(text string, n int) Input {
	in := inputFor(text)
	in.AudioBase64 = base64.StdEncoding.EncodeToString(make([]byte, n))
	return in
}

func TestVoiceWSStreamsChunksAndCollectsReply(t *testing.T) {
	a, rec := newAgent()
	f := fakes.NewVoiceWS(a)
	defer f.Close()
	ex, err := send(t, voiceTarget(f.URL()), Perception, voiceInput(fakes.ObeyMarker+" "+canary, 3500))
	if err != nil {
		t.Fatal(err)
	}
	start, chunks, bytes, _ := f.Session()
	if chunks != 4 || bytes != 3500 {
		t.Errorf("3500 bytes in 1024-byte chunks should be 4 chunks, got %d chunks / %d bytes", chunks, bytes)
	}
	if !strings.Contains(start, `"type":"session.start"`) || !strings.Contains(start, canary) {
		t.Errorf("start event: %s", start)
	}
	if ex.ResponseText != "done" || len(ex.Actions) != 1 || ex.Actions[0].Name != "send_email" {
		t.Errorf("reply: %+v", ex)
	}
	if string(ex.ReplyAudio) != "RIFFfake-reply" {
		t.Errorf("reply audio: %q", ex.ReplyAudio)
	}
	if strings.Contains(ex.RawResponse, "UklGR") || !strings.Contains(ex.RawResponse, "audio chunk omitted") {
		t.Errorf("raw response must not hold the audio: %s", ex.RawResponse)
	}
	if ex.Wire["completed"] != true || ex.Wire["chunks"] != 4 {
		t.Errorf("wire: %v", ex.Wire)
	}
	if len(rec.events()) != 1 {
		t.Error("sink event missing")
	}
}

func TestVoiceWSRefusals(t *testing.T) {
	a, _ := newAgent()
	f := fakes.NewVoiceWS(a)
	defer f.Close()
	if _, err := send(t, voiceTarget(f.URL()), Perception, inputFor("no audio")); err == nil || !strings.Contains(err.Error(), "audio artifact") {
		t.Errorf("no audio: %v", err)
	}
	huge := inputFor("x")
	huge.AudioBase64 = base64.StdEncoding.EncodeToString(make([]byte, 6<<20+1))
	if _, err := send(t, voiceTarget(f.URL()), Perception, huge); err == nil || !strings.Contains(err.Error(), "6 MiB") {
		t.Errorf("oversize audio: %v", err)
	}
	bad := inputFor("x")
	bad.AudioBase64 = "!!!"
	if _, err := send(t, voiceTarget(f.URL()), Perception, bad); err == nil {
		t.Error("bad base64")
	}
}

func TestVoiceWSTimeoutAndEventLimitSendCancel(t *testing.T) {
	a, _ := newAgent()
	stall := fakes.NewVoiceWS(a)
	stall.Stall = true
	defer stall.Close()
	tg := voiceTarget(stall.URL())
	tg.TimeoutSeconds = 1
	if _, err := send(t, tg, Perception, voiceInput("x", 10)); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("stall: %v", err)
	}
	endless := fakes.NewVoiceWS(a)
	endless.Endless = true
	defer endless.Close()
	tg = voiceTarget(endless.URL())
	tg.VoiceWS.MaxEvents = 5
	if _, err := send(t, tg, Perception, voiceInput("x", 10)); err == nil || !strings.Contains(err.Error(), "event limit") {
		t.Errorf("endless: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if _, _, _, cancelled := endless.Session(); !cancelled {
		t.Error("a failed session should send the cancel event")
	}
}

func TestReplyAudioIsKeptAndCutOutOfTheStoredReply(t *testing.T) {
	audio := []byte("RIFF....WAVEfake")
	body := `{"text":"spoken reply","result":{"audio":"` + base64.StdEncoding.EncodeToString(audio) + `"}}`
	tg := model.Target{ReplyAudioPath: "result.audio"}
	var ex model.Exchange
	Finish(tg, &ex, body)
	if string(ex.ReplyAudio) != string(audio) || ex.ResponseText != "spoken reply" {
		t.Fatalf("%q %q", ex.ReplyAudio, ex.ResponseText)
	}
	if strings.Contains(ex.RawResponse, "UklGR") || !strings.Contains(ex.RawResponse, "audio omitted: 16 bytes") {
		t.Errorf("the audio must leave the stored reply: %s", ex.RawResponse)
	}
	for _, bad := range []string{`not json`, `{"result":{"audio":"!!!"}}`, `{"result":{}}`, `{"result":{"audio":5}}`} {
		var e model.Exchange
		Finish(tg, &e, bad)
		if e.ReplyAudio != nil || e.RawResponse != bad {
			t.Errorf("%s: unusable audio leaves the reply alone", bad)
		}
	}
}
