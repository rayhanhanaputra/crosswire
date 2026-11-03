package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/rayhanhanaputra/crosswire/internal/fakes"
	"github.com/rayhanhanaputra/crosswire/internal/model"
)

func (e *env) post(path string, body any) (int, map[string]any) {
	e.t.Helper()
	resp, raw := e.do("POST", path, body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

func TestTargetTransportValidation(t *testing.T) {
	e := setup(t)
	base := func(over map[string]any) map[string]any {
		m := map[string]any{"name": "t", "environment": "staging", "endpoint": "http://127.0.0.1:1/x", "evidence": map[string]any{"forbidden_tools": []string{"x"}}}
		for k, v := range over {
			m[k] = v
		}
		return m
	}
	bridge := func(over map[string]any) map[string]any {
		b := map[string]any{"kind": "led", "stop_endpoint": "http://127.0.0.1:9910/stop", "device_id": "arduino-01", "resource": "pin-9",
			"allowed_device_ids": []string{"arduino-01"}, "allowed_resources": []string{"pin-9"}}
		for k, v := range over {
			b[k] = v
		}
		return base(map[string]any{"transport": "bridge", "endpoint": "http://127.0.0.1:9910/action", "bridge": b})
	}
	good := map[string]map[string]any{
		"ws":        base(map[string]any{"transport": "ws", "endpoint": "ws://127.0.0.1:1/x", "ws": map[string]any{"subprotocol": "agent.v1"}}),
		"wss":       base(map[string]any{"transport": "ws", "endpoint": "wss://agent.example.com/x"}),
		"tcp":       base(map[string]any{"transport": "tcp", "endpoint": "tcp://10.0.0.5:9000", "tcp": map[string]any{"framing": "length", "read_until": "idle", "idle_ms": 200}}),
		"tcp raw":   base(map[string]any{"transport": "tcp", "endpoint": "tcp://127.0.0.1:9000", "request_template": "", "tcp": map[string]any{"send": "raw", "terminator": `\r\n`}}),
		"mqtt":      base(map[string]any{"transport": "mqtt", "endpoint": "mqtt://127.0.0.1:1883", "request_template": `{"input":"{{content}}","id":"{{canary}}"}`, "mqtt": map[string]any{"publish_topic": "a/in", "response_topic": "a/{{canary}}/out", "qos": 0}}),
		"voice-ws":  base(map[string]any{"transport": "voice-ws", "endpoint": "ws://127.0.0.1:1/v", "voice_ws": map[string]any{"chunk_bytes": 4096, "chunk_template": `{"audio":"{{audio_chunk_base64}}"}`}}),
		"bridge":    bridge(nil),
		"observer":  base(map[string]any{"observer": map[string]any{"kind": "http", "url": "http://127.0.0.1:1/state", "path": "a.b", "reset_url": "http://127.0.0.1:1/reset", "nonce": "not_visible"}}),
		"browser":   base(map[string]any{"observer": map[string]any{"kind": "browser", "url": "http://127.0.0.1:1/", "selector": "#s", "screenshot": true}}),
		"direct ws": base(map[string]any{"direct_endpoint": "ws://127.0.0.1:1/chat"}),
		"mapper": base(map[string]any{"request_template": `{{mapped}}`, "mapper": map[string]any{"enabled": true, "endpoint": "https://api.example.com/v1/chat/completions", "model": "m",
			"api_key_env": "OPENAI_API_KEY", "instructions": "Return {input}.", "required_keys": []string{"input"}},
			"response_mapper": map[string]any{"enabled": true, "endpoint": "http://127.0.0.1:1/v1", "model": "m", "api_key_env": "OPENAI_API_KEY"}}),
		"production": base(map[string]any{"environment": "production", "endpoint": "https://agent.example.com/x"}),
	}
	for name, body := range good {
		if code, out := e.post("/api/targets", body); code != 201 {
			t.Errorf("%s: want 201, got %d %v", name, code, out)
		}
	}
	// Defaults and clearing of unselected blocks.
	var got model.Target
	e.json("POST", "/api/targets", base(map[string]any{"transport": "mqtt", "endpoint": "mqtt://127.0.0.1:1883", "request_template": `{"c":"{{canary}}"}`,
		"mqtt": map[string]any{"publish_topic": "a", "response_topic": "b"}, "tcp": map[string]any{"framing": "length"}}), 201, &got)
	if got.MQTT.Correlate != "nonce" || got.TCP.Framing != "" {
		t.Errorf("a {{canary}} template defaults to nonce correlation, and other transports' settings are cleared: %+v %+v", got.MQTT, got.TCP)
	}

	bad := map[string]map[string]any{
		"ws with http url":       base(map[string]any{"transport": "ws"}),
		"ws credentials":         base(map[string]any{"transport": "ws", "endpoint": "ws://u:p@127.0.0.1:1/x"}),
		"tcp no port":            base(map[string]any{"transport": "tcp", "endpoint": "tcp://127.0.0.1"}),
		"tcp bad framing":        base(map[string]any{"transport": "tcp", "endpoint": "tcp://127.0.0.1:1", "tcp": map[string]any{"framing": "frames"}}),
		"tcp bad terminator":     base(map[string]any{"transport": "tcp", "endpoint": "tcp://127.0.0.1:1", "tcp": map[string]any{"terminator": `\q`}}),
		"mqtt wildcard":          base(map[string]any{"transport": "mqtt", "endpoint": "mqtt://127.0.0.1:1883", "mqtt": map[string]any{"publish_topic": "a/#", "response_topic": "b"}}),
		"mqtt password no user":  base(map[string]any{"transport": "mqtt", "endpoint": "mqtt://127.0.0.1:1883", "mqtt": map[string]any{"publish_topic": "a", "response_topic": "b", "password_env": "MQ_PASS"}}),
		"mqtt literal password":  base(map[string]any{"transport": "mqtt", "endpoint": "mqtt://127.0.0.1:1883", "mqtt": map[string]any{"publish_topic": "a", "response_topic": "b", "username_env": "u", "password_env": "hunter 2!"}}),
		"mqtt nonce no canary":   base(map[string]any{"transport": "mqtt", "endpoint": "mqtt://127.0.0.1:1883", "mqtt": map[string]any{"publish_topic": "a", "response_topic": "b", "correlate": "nonce"}}),
		"mqtt qos 2":             base(map[string]any{"transport": "mqtt", "endpoint": "mqtt://127.0.0.1:1883", "mqtt": map[string]any{"publish_topic": "a", "response_topic": "b", "qos": 2}}),
		"voice chunk too small":  base(map[string]any{"transport": "voice-ws", "endpoint": "ws://127.0.0.1:1/v", "voice_ws": map[string]any{"chunk_bytes": 10}}),
		"voice bad template":     base(map[string]any{"transport": "voice-ws", "endpoint": "ws://127.0.0.1:1/v", "voice_ws": map[string]any{"end_template": "{nope"}}),
		"bridge no allowlist":    bridge(map[string]any{"allowed_device_ids": []string{"other"}}),
		"bridge other origin":    bridge(map[string]any{"stop_endpoint": "http://127.0.0.1:1/stop"}),
		"bridge off-host http":   base(map[string]any{"transport": "bridge", "endpoint": "http://10.1.1.1/a", "bridge": map[string]any{"kind": "led", "stop_endpoint": "http://10.1.1.1/s", "device_id": "d", "resource": "r", "allowed_device_ids": []string{"d"}, "allowed_resources": []string{"r"}}}),
		"bridge duration":        bridge(map[string]any{"duration_ms": 99999}),
		"bridge kind":            bridge(map[string]any{"kind": "radio"}),
		"observer no url":        base(map[string]any{"observer": map[string]any{"kind": "http"}}),
		"observer off-host http": base(map[string]any{"observer": map[string]any{"kind": "http", "url": "http://10.1.1.1/state"}}),
		"browser no selector":    base(map[string]any{"observer": map[string]any{"kind": "browser", "url": "http://127.0.0.1:1/"}}),
		"observer bad nonce":     base(map[string]any{"observer": map[string]any{"kind": "http", "url": "http://127.0.0.1:1/s", "nonce": "maybe"}}),
		"observer literal key":   base(map[string]any{"observer": map[string]any{"kind": "http", "url": "http://127.0.0.1:1/s", "secret_headers": map[string]string{"X-Key": "sk-live 123"}}}),
		"mapper no mapped":       base(map[string]any{"mapper": map[string]any{"enabled": true, "endpoint": "https://api.example.com/v1", "model": "m", "api_key_env": "K", "instructions": "x"}}),
		"mapper literal key":     base(map[string]any{"request_template": `{{mapped}}`, "mapper": map[string]any{"enabled": true, "endpoint": "https://api.example.com/v1", "model": "m", "api_key_env": "sk-abc def", "instructions": "x"}}),
		"mapper http off-host":   base(map[string]any{"request_template": `{{mapped}}`, "mapper": map[string]any{"enabled": true, "endpoint": "http://api.example.com/v1", "model": "m", "api_key_env": "K", "instructions": "x"}}),
		"mapper no instructions": base(map[string]any{"request_template": `{{mapped}}`, "mapper": map[string]any{"enabled": true, "endpoint": "https://api.example.com/v1", "model": "m", "api_key_env": "K"}}),
		"response mapper no key": base(map[string]any{"response_mapper": map[string]any{"enabled": true, "endpoint": "https://api.example.com/v1", "model": "m"}}),
		"credential header":      base(map[string]any{"headers": map[string]string{"Authorization": "Bearer abc"}}),
		"cookie header":          base(map[string]any{"headers": map[string]string{"Cookie": "a=b"}}),
		"host secret header":     base(map[string]any{"secret_headers": map[string]string{"Host": "H"}}),
		"credentials in url":     base(map[string]any{"endpoint": "http://user:pw@127.0.0.1:1/x"}),
		"direct ftp":             base(map[string]any{"direct_endpoint": "ftp://127.0.0.1/x"}),
		"production http":        base(map[string]any{"environment": "production", "endpoint": "http://agent.example.com/x"}),
		"production ws":          base(map[string]any{"environment": "production", "transport": "ws", "endpoint": "ws://agent.example.com/x"}),
		"production tcp":         base(map[string]any{"environment": "production", "transport": "tcp", "endpoint": "tcp://10.0.0.1:9"}),
		"production direct http": base(map[string]any{"environment": "production", "endpoint": "https://agent.example.com/x", "direct_endpoint": "http://agent.example.com/chat"}),
		"unknown transport":      base(map[string]any{"transport": "carrier-pigeon"}),
	}
	for name, body := range bad {
		code, out := e.post("/api/targets", body)
		t.Logf("%-24s %v", name, out["error"])
		if code != 400 {
			t.Errorf("%s: want 400, got %d %v", name, code, out)
		}
	}
}

func TestPreviewDescribesTheTransportAndTheMapper(t *testing.T) {
	e := setup(t)
	var tg model.Target
	e.json("POST", "/api/targets", map[string]any{"name": "mq", "environment": "staging", "transport": "mqtt", "endpoint": "mqtt://127.0.0.1:1883",
		"request_template": `{"in":{{mapped}},"id":"{{canary}}"}`, "evidence": map[string]any{"forbidden_tools": []string{"x"}},
		"mqtt":   map[string]any{"publish_topic": "a/{{canary}}", "response_topic": "b", "username_env": "MQ_USER"},
		"mapper": map[string]any{"enabled": true, "endpoint": "https://api.example.com/v1", "model": "gpt-x", "api_key_env": "K", "instructions": "x"}}, 201, &tg)
	var p map[string]any
	e.json("POST", "/api/preview", map[string]any{"target_id": tg.ID, "adapter": "ble-advert", "fields": map[string]any{"name": "Lock"}}, 200, &p)
	wire := p["wire"].(map[string]any)
	mq := wire["mqtt"].(map[string]any)
	if wire["transport"] != "mqtt" || mq["publish_topic"] != "a/cw-preview" || mq["username"] != "<secret:MQ_USER>" || mq["qos"] != 1.0 {
		t.Errorf("wire: %v", wire)
	}
	if p["mapper"] == nil || !strings.Contains(p["mapper"].(map[string]any)["note"].(string), "never evidence") {
		t.Errorf("mapper note: %v", p["mapper"])
	}
	if p["artifact"].(map[string]any)["kind"] != "application/x-ble-ad" || p["artifact_base64"] == "" {
		t.Errorf("artifact: %v", p["artifact"])
	}
	if !strings.Contains(p["request"].(map[string]any)["id"].(string), "cw-preview") {
		t.Errorf("request: %v", p["request"])
	}

	b := fakes.NewBridge("led", "arduino-01", "pin-9")
	defer b.Close()
	e.json("POST", "/api/targets", map[string]any{"name": "led", "environment": "staging", "transport": "bridge", "endpoint": b.ActionURL(),
		"bridge": map[string]any{"kind": "led", "stop_endpoint": b.StopURL(), "device_id": "arduino-01", "resource": "pin-9", "allowed_device_ids": []string{"arduino-01"}, "allowed_resources": []string{"pin-9"}},
	}, 201, &tg)
	e.json("POST", "/api/preview", map[string]any{"target_id": tg.ID, "adapter": "morse-led", "fields": map[string]any{"text": "sos"}}, 200, &p)
	br := p["wire"].(map[string]any)["bridge"].(map[string]any)
	if br["operation"] != "led_sequence" || br["mandatory_stop"] == nil || p["request"].(map[string]any)["resource"] != "pin-9" {
		t.Errorf("bridge preview: %v", p["wire"])
	}
}

func TestRunOverWebSocketAndArtifactTypes(t *testing.T) {
	e := setup(t)
	agent := &fakes.Agent{Name: "ws-agent"}
	var tg model.Target
	// Report straight to this server's sink over HTTP, the way a real agent would.
	agent.Report = func(ev model.SinkEvent) {
		b, _ := json.Marshal(ev)
		_, _ = http.Post(e.srv.URL+"/api/sink", "application/json", strings.NewReader(string(b)))
	}
	ws := fakes.NewWS(agent)
	defer ws.Close()
	e.json("POST", "/api/targets", map[string]any{"name": "ws", "environment": "staging", "transport": "ws", "endpoint": ws.URL(),
		"request_template": `{"input":"{{content}}"}`, "evidence": map[string]any{"forbidden_tools": []string{"send_email"}}}, 201, &tg)
	var run model.Run
	e.json("POST", "/api/runs", map[string]any{"target_id": tg.ID, "adapter": "nfc-tag", "repeats": 5, "concurrency": 2,
		"fields":         map[string]any{"text": fakes.ObeyMarker + " {{canary}}"},
		"control_fields": map[string]any{"text": "all clear {{canary}}"}}, 200, &run)
	if run.Verdict.Outcome != "confirmed" || run.Channel.Artifact.Kind != "application/x-ndef" {
		t.Fatalf("%s: %s", run.Verdict.Outcome, run.Verdict.Explanation)
	}
	resp, raw := e.do("GET", "/api/runs/"+run.ID+"/artifacts/injected/0", nil)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/octet-stream" || len(raw) < 5 || raw[0]&0x80 == 0 {
		t.Errorf("ndef artifact: %d %s %x", resp.StatusCode, resp.Header.Get("Content-Type"), raw)
	}
	// The bundle holds the stored artifacts with their extension.
	_, zipRaw := e.do("GET", "/api/runs/"+run.ID+"/bundle", nil)
	if !strings.Contains(string(zipRaw), "injected-0.ndef") {
		t.Error("bundle should list the ndef artifact")
	}
	if code, _ := e.post("/api/stop", nil); code != 200 {
		t.Errorf("stop with nothing running: %d", code)
	}
}

func TestKillSwitchCancelsARunInFlight(t *testing.T) {
	e := setup(t)
	agent := &fakes.Agent{}
	v := fakes.NewVoiceWS(agent)
	v.Stall = true
	defer v.Close()
	var tg model.Target
	no := false
	e.json("POST", "/api/targets", map[string]any{"name": "slow", "environment": "staging", "transport": "voice-ws", "endpoint": v.URL(), "timeout_seconds": 30,
		"request_template": `{"type":"start"}`, "evidence": map[string]any{"forbidden_tools": []string{"x"}, "require_canary": &no}}, 201, &tg)
	wav := "UklGRiQAAABXQVZFZm10IBAAAAABAAEAgD4AAAB9AAACABAAZGF0YQAAAAA="
	var started map[string]string
	e.json("POST", "/api/runs?async=1", map[string]any{"target_id": tg.ID, "adapter": "audio-upload", "repeats": 3,
		"fields": map[string]any{"audio_base64": wav}}, 202, &started)
	time.Sleep(500 * time.Millisecond)
	begin := time.Now()
	code, out := e.post("/api/stop", nil)
	if code != 200 || out["cancelled"] != 1.0 {
		t.Fatalf("stop: %d %v", code, out)
	}
	for i := 0; i < 100; i++ {
		var run model.Run
		resp, raw := e.do("GET", "/api/runs/"+started["run_id"], nil)
		if resp.StatusCode == 200 {
			_ = json.Unmarshal(raw, &run)
			if run.Verdict.Outcome == "error" {
				if time.Since(begin) > 5*time.Second {
					t.Errorf("the run took %v to stop", time.Since(begin))
				}
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the cancelled run never finished")
}

func TestPermissionsPolicyAllowsTheMicrophoneForThisPageOnly(t *testing.T) {
	e := setup(t)
	resp, _ := e.do("GET", "/", nil)
	if p := resp.Header.Get("Permissions-Policy"); !strings.Contains(p, "microphone=(self)") || !strings.Contains(p, "camera=()") {
		t.Errorf("Permissions-Policy: %q", p)
	}
}

var _ = context.Background
