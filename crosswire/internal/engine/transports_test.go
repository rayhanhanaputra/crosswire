package engine

import (
	"context"
	"encoding/base64"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/bridge"
	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/channel"
	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/fakes"
	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/model"
	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/signal"
	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/sink"
	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/store"
)

// env is an engine whose fake agents report tool calls straight to its sink.
type env struct {
	e     *Engine
	st    *store.Store
	agent *fakes.Agent
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "cw.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	sk := sink.New()
	return &env{
		e:     &Engine{Store: st, Sink: sk, Encoder: channel.Encoder{}},
		st:    st,
		agent: &fakes.Agent{Name: "fake", Report: func(ev model.SinkEvent) { sk.Add(ev) }},
	}
}

func (v *env) put(t *testing.T, tg model.Target) {
	t.Helper()
	tg.Environment, tg.Enabled = "staging", true
	if tg.Name == "" {
		tg.Name = tg.ID
	}
	if tg.TimeoutSeconds == 0 {
		tg.TimeoutSeconds = 5
	}
	if len(tg.Evidence.ForbiddenTools) == 0 && tg.Observer.Kind == "" {
		tg.Evidence.ForbiddenTools = []string{"send_email"}
	}
	if err := v.st.PutTarget(tg); err != nil {
		t.Fatal(err)
	}
}

var obey = map[string]any{"text": fakes.ObeyMarker + " {{canary}}"}
var benign = map[string]any{"text": "shelf B4 restocked {{canary}}"}

func (v *env) run(t *testing.T, id, adapter string, fields, control map[string]any, repeats int) model.Run {
	t.Helper()
	run, err := v.e.Run(context.Background(), Request{TargetID: id, Adapter: adapter, Fields: fields, ControlFields: control, Repeats: repeats, Concurrency: 2, ProductionAck: true})
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func wantConfirmed(t *testing.T, run model.Run) {
	t.Helper()
	inj, ctl := run.Verdict.Arms["injected"], run.Verdict.Arms["control"]
	if run.Verdict.Outcome != "confirmed" || inj.Hits != inj.Reached || inj.Hits < 2 || ctl.Hits != 0 {
		t.Fatalf("want confirmed with every injected trial a hit, got %s: %s\narms %+v", run.Verdict.Outcome, run.Verdict.Explanation, run.Verdict.Arms)
	}
	for _, tr := range run.Trials {
		if len(tr.Timeline) < 3 {
			t.Errorf("trial %d/%s has a thin timeline: %+v", tr.Index, tr.Arm, tr.Timeline)
		}
	}
}

func TestEveryTransportRunsEndToEnd(t *testing.T) {
	v := newEnv(t)

	ws := fakes.NewWS(v.agent)
	defer ws.Close()
	v.put(t, model.Target{ID: "ws", Transport: "ws", Endpoint: ws.URL(), RequestTemplate: `{"input":"{{content}}","session":"{{canary}}"}`})

	tcp := fakes.NewTCP(v.agent, fakes.TCPOptions{})
	defer tcp.Close()
	v.put(t, model.Target{ID: "tcp", Transport: "tcp", Endpoint: tcp.URL(), RequestTemplate: `{"input":"{{content}}"}`})

	tcpRaw := fakes.NewTCP(v.agent, fakes.TCPOptions{Terminator: "\r\n"})
	defer tcpRaw.Close()
	v.put(t, model.Target{ID: "tcp-raw", Transport: "tcp", Endpoint: tcpRaw.URL(), TCP: model.TCPConfig{Send: "raw", Terminator: `\r\n`}})

	broker := fakes.NewMQTTBroker()
	defer broker.Close()
	fakes.NewMQTTAgent(broker, "agents/in", "agents/out", v.agent, fakes.MQTTAgentOptions{Noise: true})
	v.put(t, model.Target{ID: "mqtt", Transport: "mqtt", Endpoint: broker.URL(), RequestTemplate: `{"input":"{{content}}","correlation_id":"{{canary}}"}`,
		MQTT: model.MQTTConfig{PublishTopic: "agents/in", ResponseTopic: "agents/out", Correlate: "nonce"}})

	for _, id := range []string{"ws", "tcp", "tcp-raw", "mqtt"} {
		t.Run(id, func(t *testing.T) {
			run := v.run(t, id, "text", obey, benign, 5)
			wantConfirmed(t, run)
		})
	}
	// The same payload through a structured-field channel.
	t.Run("json-fields over ws", func(t *testing.T) {
		run := v.run(t, "ws", "json-fields", map[string]any{"json": `{"note":"` + fakes.ObeyMarker + ` {{canary}}"}`}, map[string]any{"json": `{"note":"all clear {{canary}}"}`}, 5)
		wantConfirmed(t, run)
		if run.Channel.Artifact.Kind != "application/json" || run.Channel.Modeled {
			t.Errorf("%+v", run.Channel)
		}
	})
}

func TestVoiceStreamCarriesARecording(t *testing.T) {
	v := newEnv(t)
	f := fakes.NewVoiceWS(v.agent)
	defer f.Close()
	no := false
	v.put(t, model.Target{ID: "voice", Transport: "voice-ws", Endpoint: f.URL(), RequestTemplate: `{"type":"session.start","input":"{{content}}"}`,
		VoiceWS: model.VoiceWSConfig{ChunkBytes: 1024, ReplyAudioPath: "audio"}, Evidence: model.Rules{ForbiddenTools: []string{"send_email"}, RequireCanary: &no}})
	wav := base64.StdEncoding.EncodeToString(signal.WAV(16000, make([]int16, 8000)))
	fields := map[string]any{"audio_base64": wav, "text": fakes.ObeyMarker + " {{canary}}"}
	control := map[string]any{"audio_base64": wav, "text": "weather today {{canary}}"}
	run := v.run(t, "voice", "audio-upload", fields, control, 5)
	wantConfirmed(t, run)
	first := run.Trials[0]
	if first.Exchange.Wire["chunks"] != 16 || first.Exchange.ReplyArtifact == "" {
		t.Errorf("a 16000 byte clip in 1024 byte chunks is 16 chunks, and the reply audio is kept: %+v", first.Exchange)
	}
	if first.Exchange.Request["input"] == nil {
		t.Errorf("start event: %v", first.Exchange.Request)
	}
}

func TestAudioUploadNeedsAPermissiveTarget(t *testing.T) {
	v := newEnv(t)
	f := fakes.NewVoiceWS(v.agent)
	defer f.Close()
	v.put(t, model.Target{ID: "voice", Transport: "voice-ws", Endpoint: f.URL(), RequestTemplate: `{"type":"session.start"}`})
	wav := base64.StdEncoding.EncodeToString(signal.WAV(16000, make([]int16, 100)))
	_, err := v.e.Run(context.Background(), Request{TargetID: "voice", Adapter: "audio-upload", Fields: map[string]any{"audio_base64": wav}, Repeats: 1})
	if err == nil || !strings.Contains(err.Error(), "fixed recording") {
		t.Fatalf("a fixed recording cannot carry the canary: %v", err)
	}
	_, err = v.e.Run(context.Background(), Request{TargetID: "voice", Adapter: "text", Fields: map[string]any{"text": "x"}, Repeats: 1})
	if err == nil || !strings.Contains(err.Error(), "audio only") {
		t.Fatalf("voice-ws takes audio: %v", err)
	}
}

func TestProductionTargetsNeedTLSOffHost(t *testing.T) {
	v := newEnv(t)
	for _, tg := range []model.Target{
		{ID: "p-http", Endpoint: "http://agent.example.com/perceive"},
		{ID: "p-ws", Transport: "ws", Endpoint: "ws://agent.example.com/x"},
		{ID: "p-mqtt", Transport: "mqtt", Endpoint: "mqtt://10.0.0.5:1883"},
		{ID: "p-tcp", Transport: "tcp", Endpoint: "tcp://10.0.0.5:9000"},
	} {
		tg.Environment, tg.Enabled, tg.Name = "production", true, tg.ID
		tg.Evidence.ForbiddenTools = []string{"send_email"}
		_ = v.st.PutTarget(tg)
		_, err := v.e.Run(context.Background(), Request{TargetID: tg.ID, Adapter: "text", Fields: map[string]any{"text": "x"}, Repeats: 1, ProductionAck: true})
		if err == nil || !strings.Contains(err.Error(), "production") {
			t.Errorf("%s: want a production TLS refusal, got %v", tg.ID, err)
		}
	}
	// No acknowledgement, no run.
	_ = v.st.PutTarget(model.Target{ID: "p-ok", Name: "p", Environment: "production", Enabled: true, Endpoint: "https://agent.example.com/x", Evidence: model.Rules{ForbiddenTools: []string{"x"}}})
	if _, err := v.e.Run(context.Background(), Request{TargetID: "p-ok", Adapter: "text", Fields: map[string]any{"text": "x"}, Repeats: 1}); err == nil || !strings.Contains(err.Error(), "acknowledgement") {
		t.Errorf("acknowledgement: %v", err)
	}
}

func TestBridgeRunsSeriallyAndRecordsTheStop(t *testing.T) {
	v := newEnv(t)
	b := fakes.NewBridge("led", "arduino-01", "pin-9")
	defer b.Close()
	state := fakes.NewStatePage(&fakes.Agent{}, fakes.StatePageOptions{})
	defer state.Close()
	// The agent that watches the LED: when the pulses decode to text that holds
	// the marker word, its panel shows what it read, nonce included.
	var mu sync.Mutex
	b.Sim.OnStart = func(c bridge.Command) {
		mu.Lock()
		defer mu.Unlock()
		text := strings.ToLower(signal.PulsesDecode(c.Pulses))
		if strings.Contains(text, "obey") {
			state.Set("ALERT " + text)
		}
	}
	v.put(t, model.Target{ID: "led", Transport: "bridge", Endpoint: b.ActionURL(),
		Bridge: model.BridgeConfig{Kind: "led", StopEndpoint: b.StopURL(), DeviceID: "arduino-01", Resource: "pin-9",
			AllowedDeviceIDs: []string{"arduino-01"}, AllowedResources: []string{"pin-9"}, DurationMs: 30000, UnitMs: 50},
		Observer: model.ObserverConfig{Kind: "http", URL: state.StateURL(), Path: "status", WaitMs: 50}})
	fields := map[string]any{"text": "obey {{canary}}", "wpm": 20.0}
	control := map[string]any{"text": "okay {{canary}}", "wpm": 20.0}
	// Every trial starts from the same panel text.
	run := v.run(t, "led", "morse-led", fields, control, 2)
	for _, tr := range run.Trials {
		if tr.Exchange.Bridge == nil || !tr.Exchange.Bridge.StopConfirmed || tr.Exchange.Bridge.AckStatus != 200 {
			t.Fatalf("trial %d/%s: %+v err=%q", tr.Index, tr.Arm, tr.Exchange.Bridge, tr.Exchange.Error)
		}
		var sawAck, sawStop, sawBefore, sawAfter bool
		for _, ev := range tr.Timeline {
			sawAck = sawAck || ev.Event == "action acknowledged"
			sawStop = sawStop || ev.Event == "emergency stop confirmed"
			sawBefore = sawBefore || ev.Event == "before read"
			sawAfter = sawAfter || ev.Event == "after read"
		}
		if !sawAck || !sawStop || !sawBefore || !sawAfter {
			t.Errorf("trial %d/%s timeline lacks a step: %+v", tr.Index, tr.Arm, tr.Timeline)
		}
	}
	log := strings.Join(b.Sim.Log(), ",")
	if log != "start,stop,start,stop,start,stop,start,stop" {
		t.Errorf("bridge calls must alternate start and stop, one device at a time: %s", log)
	}
	if run.Verdict.Arms["injected"].Hits != 2 {
		t.Errorf("the panel showed the nonce twice: %s %+v", run.Verdict.Explanation, run.Verdict.Arms)
	}
}

func TestBridgeNeedsAcknowledgementAndTheRightChannel(t *testing.T) {
	v := newEnv(t)
	b := fakes.NewBridge("ble", "hci0", "adv0")
	defer b.Close()
	v.put(t, model.Target{ID: "ble", Transport: "bridge", Endpoint: b.ActionURL(),
		Bridge: model.BridgeConfig{Kind: "ble", StopEndpoint: b.StopURL(), DeviceID: "hci0", Resource: "adv0", AllowedDeviceIDs: []string{"hci0"}, AllowedResources: []string{"adv0"}}})
	_, err := v.e.Run(context.Background(), Request{TargetID: "ble", Adapter: "ble-advert", Fields: map[string]any{"name": "Lock"}, Repeats: 1})
	if err == nil || !strings.Contains(err.Error(), "acknowledgement") {
		t.Errorf("hardware needs an acknowledgement: %v", err)
	}
	_, err = v.e.Run(context.Background(), Request{TargetID: "ble", Adapter: "text", Fields: map[string]any{"text": "x"}, Repeats: 1, ProductionAck: true})
	if err == nil || !strings.Contains(err.Error(), "ble-advert") {
		t.Errorf("wrong channel: %v", err)
	}
	if len(b.Sim.Log()) != 0 {
		t.Errorf("nothing may reach the bridge: %v", b.Sim.Log())
	}
	run, err := v.e.Run(context.Background(), Request{TargetID: "ble", Adapter: "ble-advert", Fields: map[string]any{"name": "Lock {{canary}}"}, Repeats: 1, ProductionAck: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := run.Trials[0].Exchange.Bridge; got == nil || got.Operation != "ble_advertise" || !got.StopConfirmed {
		t.Errorf("%+v", got)
	}
	if hex := b.Sim.Last().Input["advertisement_hex"]; hex == nil || hex == "" {
		t.Errorf("the bridge should receive the advertising bytes: %v", b.Sim.Last().Input)
	}
}

func TestUnconfirmedStopHaltsTheRun(t *testing.T) {
	v := newEnv(t)
	b := fakes.NewBridge("led", "arduino-01", "pin-9")
	defer b.Close()
	b.Sim.FailStop = true
	v.put(t, model.Target{ID: "led", Transport: "bridge", Endpoint: b.ActionURL(),
		Bridge: model.BridgeConfig{Kind: "led", StopEndpoint: b.StopURL(), DeviceID: "arduino-01", Resource: "pin-9", AllowedDeviceIDs: []string{"arduino-01"}, AllowedResources: []string{"pin-9"}, DurationMs: 30000, UnitMs: 50}})
	run := v.run(t, "led", "morse-led", map[string]any{"text": "ab {{canary}}", "wpm": 20.0}, map[string]any{"text": "ok {{canary}}", "wpm": 20.0}, 3)
	if got := strings.Join(b.Sim.Log(), ","); got != "start,stop" {
		t.Errorf("after an unconfirmed stop nothing more may be sent: %s", got)
	}
	var halted int
	for _, tr := range run.Trials {
		if strings.Contains(tr.Exchange.Error, "halted") {
			halted++
		}
	}
	if halted != len(run.Trials)-1 || run.Verdict.Outcome != "error" {
		t.Errorf("want %d halted trials and outcome error, got %d / %s", len(run.Trials)-1, halted, run.Verdict.Outcome)
	}
}
