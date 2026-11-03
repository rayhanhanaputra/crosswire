package connector

import (
	"fmt"

	"github.com/rayhanhanaputra/crosswire/internal/model"
)

// Preview describes, without sending anything, how the perception path of a
// target will carry an input: the transport and its wire settings. The
// rendered request body is returned separately by Render.
func Preview(t model.Target, in Input) map[string]any {
	out := map[string]any{"transport": PathTransport(t, Perception, t.Endpoint), "endpoint": t.Endpoint}
	switch out["transport"] {
	case "tcp":
		c := t.TCP
		out["tcp"] = map[string]any{"send": orDefault(c.Send, "json"), "framing": orDefault(c.Framing, "terminator"),
			"terminator": orDefault(c.Terminator, `\n`), "read_until": orDefault(c.ReadUntil, "terminator"), "tls": c.TLS}
	case "mqtt":
		c := t.MQTT
		qos := 1
		if c.QoS != nil {
			qos = *c.QoS
		}
		out["mqtt"] = map[string]any{"publish_topic": expandTopic(c.PublishTopic, in.Canary), "response_topic": expandTopic(c.ResponseTopic, in.Canary),
			"qos": qos, "retain": false, "correlate": orDefault(c.Correlate, "nonce"),
			"username": envRef(c.UsernameEnv), "password": envRef(c.PasswordEnv)}
	case "voice-ws":
		d := VoiceDefaults()
		chunk := t.VoiceWS.ChunkBytes
		if chunk <= 0 {
			chunk = d.ChunkBytes
		}
		n := len(in.AudioBase64) / 4 * 3
		out["voice_ws"] = map[string]any{"audio_bytes": n, "chunk_bytes": chunk, "chunks": (n + chunk - 1) / chunk,
			"done_when": fmt.Sprintf("%s = %s", orDefault(t.VoiceWS.DonePath, d.DonePath), orDefault(t.VoiceWS.DoneValue, d.DoneValue))}
	case "bridge":
		if action, stop, err := RenderBridge(t, in); err == nil {
			out["bridge"] = map[string]any{"kind": t.Bridge.Kind, "operation": model.BridgeOperation(t.Bridge.Kind), "device_id": t.Bridge.DeviceID,
				"resource": t.Bridge.Resource, "duration_ms": t.Bridge.DurationMs, "action": action, "mandatory_stop": map[string]any{"url": t.Bridge.StopEndpoint, "body": stop}}
		} else {
			out["bridge_error"] = err.Error()
		}
	}
	if len(t.SecretHeaders) > 0 {
		h := map[string]any{}
		for k, env := range t.SecretHeaders {
			h[k] = envRef(env)
		}
		out["secret_headers"] = h
	}
	return out
}

func envRef(name string) any {
	if name == "" {
		return nil
	}
	return "<secret:" + name + ">"
}
