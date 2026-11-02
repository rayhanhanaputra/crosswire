// Package connector delivers an encoded payload to a target and captures the
// reply. HTTP is what most agent gateways speak and what the reference
// targets expose. WebSocket, raw TCP, MQTT, realtime voice over WebSocket,
// SIP and the physical device bridge reuse the same Target model, request
// templates, response paths and limits. docs/CONTRACT.md section 1 lists them.
package connector

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/guard"
	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/model"
)

const maxResponseBytes = 2 << 20

// Input is what the connector has to deliver.
type Input struct {
	Source      string
	Content     string
	ImageBase64 string
	AudioBase64 string
	// FileBase64 carries a document (PDF, ICS, EML); FileType is its MIME type.
	FileBase64 string
	FileType   string
	Canary     string
	// ArtifactBase64, ArtifactHex and ArtifactType carry the raw bytes of any
	// artifact (NDEF, BLE advertising data, JSON message) and its media type.
	ArtifactBase64 string
	ArtifactHex    string
	ArtifactType   string
	// BlinkUnits is a raw JSON array of ["on"|"off", units] steps for the
	// binary-toggle and Morse channels. UnitMs is the length of one unit.
	BlinkUnits string
	UnitMs     int
	// Mapped is the input mapper's output for {{mapped}}; nil without a mapper.
	Mapped map[string]any
	// Extra holds further placeholders (device_id, resource and so on).
	// Values are substituted as JSON-safe strings.
	Extra map[string]string
	// AfterAction, when set, runs on a bridge exchange after the bridge has
	// acknowledged the action and before the stop request: the stimulus is
	// still active, so this is where the engine waits and reads the observer.
	AfterAction func(ctx context.Context) error
}

// Arm selects which of the target's two paths to use.
type Arm string

const (
	// Perception is the physical channel (target.Endpoint).
	Perception Arm = "perception"
	// Direct is the chat path (target.DirectEndpoint).
	Direct Arm = "direct"
	// Trigger is the second phase of a stored-content trial (target.TriggerEndpoint).
	Trigger Arm = "trigger"
)

// RenderTrigger fills the trigger template. The default asks nothing
// specific; a real target names the routine request its users make.
func RenderTrigger(t model.Target, in Input) (map[string]any, error) {
	return render(t.TriggerTemplate, `{"message":"Anything I should handle?","session":"{{canary}}"}`, in)
}

// Render fills the target's request template. Placeholders are replaced with
// JSON-safe strings so a payload containing quotes cannot break the request.
func Render(t model.Target, in Input) (map[string]any, error) {
	return render(t.RequestTemplate, `{"source":"{{source}}","input":"{{content}}"}`, in)
}

// RenderDirect fills the chat-path template.
func RenderDirect(t model.Target, in Input) (map[string]any, error) {
	return render(t.DirectTemplate, `{"message":"{{content}}"}`, in)
}

func render(tpl, def string, in Input) (map[string]any, error) {
	if strings.TrimSpace(tpl) == "" {
		tpl = def
	}
	q := func(s string) string {
		enc, _ := json.Marshal(s)
		return string(enc[1 : len(enc)-1])
	}
	blink := in.BlinkUnits
	if blink == "" {
		blink = "[]"
	}
	unit := in.UnitMs
	if unit <= 0 {
		unit = 200
	}
	mapped := "null"
	if in.Mapped != nil {
		enc, _ := json.Marshal(in.Mapped)
		mapped = string(enc)
	}
	pairs := []string{
		"{{content}}", q(in.Content),
		"{{content_json}}", jsonOrQuoted(in.Content),
		"{{source}}", q(in.Source),
		"{{image_base64}}", in.ImageBase64,
		"{{audio_base64}}", in.AudioBase64,
		"{{file_base64}}", in.FileBase64,
		"{{file_type}}", q(in.FileType),
		"{{canary}}", q(in.Canary),
		"{{artifact_base64}}", in.ArtifactBase64,
		"{{artifact_hex}}", in.ArtifactHex,
		"{{artifact_type}}", q(in.ArtifactType),
		"{{blink_units}}", blink,
		"{{unit_ms}}", strconv.Itoa(unit),
		"{{mapped}}", mapped,
	}
	for k, v := range in.Extra {
		pairs = append(pairs, "{{"+k+"}}", q(v))
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(strings.NewReplacer(pairs...).Replace(tpl)), &body); err != nil {
		return nil, fmt.Errorf("request template is not valid JSON after substitution: %w", err)
	}
	return body, nil
}

func jsonOrQuoted(s string) string {
	if json.Valid([]byte(s)) {
		return s
	}
	enc, _ := json.Marshal(s)
	return string(enc)
}

// Send performs one exchange on the chosen arm. The perception arm follows
// the target's Transport; the direct and trigger arms follow the scheme of
// their own endpoint. HTTP refuses redirects. The loopback-only rule applies
// to Crosswire's own API, not to targets, which the operator configured
// explicitly; the address checks in package guard apply to every connection.
func Send(ctx context.Context, t model.Target, arm Arm, in Input) (model.Exchange, error) {
	var ex model.Exchange
	endpoint := t.Endpoint
	var body map[string]any
	var err error
	switch arm {
	case Direct:
		endpoint = t.DirectEndpoint
		if endpoint == "" {
			return ex, errors.New("target has no direct (chat) endpoint")
		}
		body, err = RenderDirect(t, in)
	case Trigger:
		endpoint = t.TriggerEndpoint
		if endpoint == "" {
			return ex, errors.New("target has no trigger endpoint")
		}
		body, err = RenderTrigger(t, in)
	default:
		if t.Transport == "sip" {
			return sendSIP(ctx, t, in)
		}
		if t.Transport == "bridge" {
			return sendBridge(ctx, t, in)
		}
		body, err = Render(t, in)
	}
	if err != nil {
		return ex, err
	}
	ex.Request = body
	switch PathTransport(t, arm, endpoint) {
	case "ws":
		return sendWS(ctx, t, endpoint, body, in)
	case "voice-ws":
		return sendVoiceWS(ctx, t, endpoint, body, in)
	case "tcp":
		return sendTCP(ctx, t, endpoint, body, in)
	case "mqtt":
		return sendMQTT(ctx, t, endpoint, body, in)
	}
	return sendHTTP(ctx, t, endpoint, body, ex)
}

// PathTransport names the transport used for one arm. The perception arm uses
// the target's Transport; the other arms use the scheme of their endpoint.
func PathTransport(t model.Target, arm Arm, endpoint string) string {
	if arm == Perception || arm == "" {
		switch t.Transport {
		case "ws", "tcp", "mqtt", "voice-ws", "sip", "bridge":
			return t.Transport
		}
		return "http"
	}
	switch {
	case strings.HasPrefix(endpoint, "ws://"), strings.HasPrefix(endpoint, "wss://"):
		return "ws"
	case strings.HasPrefix(endpoint, "tcp://"):
		return "tcp"
	}
	return "http"
}

// StaticAndSecretHeaders returns the target's static headers with the secret
// ones resolved from the environment. A secret whose variable is unset is an
// error naming the variable, never the value.
func StaticAndSecretHeaders(t model.Target) (map[string]string, error) {
	out := map[string]string{}
	for k, v := range t.Headers {
		out[k] = v
	}
	for k, envName := range t.SecretHeaders {
		v := os.Getenv(envName)
		if v == "" {
			return nil, fmt.Errorf("secret header %s: environment variable %s is not set", k, envName)
		}
		out[k] = v
	}
	return out, nil
}

// Timeout is the target's per-exchange timeout, 60 s when unset.
func Timeout(t model.Target) time.Duration {
	d := time.Duration(t.TimeoutSeconds) * time.Second
	if d <= 0 {
		d = 60 * time.Second
	}
	return d
}

// Finish parses a raw reply into the exchange with the target's response paths.
func Finish(t model.Target, ex *model.Exchange, raw string) {
	if t.ReplyAudioPath != "" {
		raw = takeReplyAudio(t.ReplyAudioPath, raw, ex)
	}
	ex.RawResponse = raw
	ex.ResponseText, ex.Actions, ex.Perceived = Parse(raw, t.ResponsePath, t.ActionsPath, t.PerceivedPath)
}

func sendHTTP(ctx context.Context, t model.Target, endpoint string, body map[string]any, ex model.Exchange) (model.Exchange, error) {
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ex, errors.New("target endpoint must be an absolute http(s) URL")
	}
	enc, _ := json.Marshal(body)
	method := t.Method
	if method == "" {
		method = http.MethodPost
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(enc))
	if err != nil {
		return ex, err
	}
	req.Header.Set("Content-Type", "application/json")
	headers, err := StaticAndSecretHeaders(t)
	if err != nil {
		return ex, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{
		Timeout: Timeout(t),
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("redirects are not followed")
		},
		Transport: &http.Transport{
			DialContext:         guard.DialContext(true, 10*time.Second),
			TLSHandshakeTimeout: 10 * time.Second,
		},
	}
	start := time.Now()
	resp, err := client.Do(req)
	ex.DurationMs = int(time.Since(start).Milliseconds())
	if err != nil {
		ex.Error = err.Error()
		return ex, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if len(raw) > maxResponseBytes {
		ex.Error = "response exceeds 2 MiB"
		return ex, errors.New(ex.Error)
	}
	ex.Status = resp.StatusCode
	Finish(t, &ex, string(raw))
	return ex, nil
}

// sendSIP plays the audio artifact into a call. There is no response body
// to parse: the far end's audio is kept, and the sink is the evidence.
func sendSIP(ctx context.Context, t model.Target, in Input) (model.Exchange, error) {
	var ex model.Exchange
	if in.AudioBase64 == "" {
		return ex, errors.New("sip transport needs an audio artifact (speech or dtmf adapter)")
	}
	wav, err := base64.StdEncoding.DecodeString(in.AudioBase64)
	if err != nil {
		return ex, errors.New("audio artifact is not valid base64")
	}
	ex.Request = map[string]any{"transport": "sip", "invite": t.Endpoint, "codec": "PCMU/8000", "audio_bytes": len(wav)}
	start := time.Now()
	call, err := DialSession(ctx, t.SIP, t.Endpoint, wav, in.Canary)
	ex.DurationMs = int(time.Since(start).Milliseconds())
	ex.Status = call.Status
	raw, _ := json.Marshal(call)
	ex.RawResponse = string(raw)
	ex.ReplyAudio = call.ReplyAudio
	if err != nil {
		ex.Error = err.Error()
		return ex, err
	}
	if call.Error != "" {
		// The target's handler failed after answering. Treat the call as a
		// trial that measured nothing, the way an HTTP error body is.
		ex.Error = "target error: " + call.Error
		return ex, errors.New(ex.Error)
	}
	ex.ResponseText = fmt.Sprintf("call answered (%d %s): played %.1f s, heard %.1f s", call.Status, call.Reason, call.SentSeconds, call.ReceivedSeconds)
	if call.RemoteHungUp {
		ex.ResponseText += ", far end hung up"
	}
	return ex, nil
}

// Parse extracts the reply text, the action list and the perceived text
// from a response body using dot paths ("result.text", "actions",
// "choices.0.message.content"). Empty paths use the contract defaults
// (text / actions / perceived). A non-JSON body is returned whole as the text.
func Parse(raw, responsePath, actionsPath, perceivedPath string) (string, []model.Action, string) {
	var doc any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return raw, nil, ""
	}
	if responsePath == "" {
		responsePath = "text"
	}
	if actionsPath == "" {
		actionsPath = "actions"
	}
	if perceivedPath == "" {
		perceivedPath = "perceived"
	}
	text := raw
	if v, ok := lookup(doc, responsePath); ok {
		text = asString(v)
	}
	perceived := ""
	if v, ok := lookup(doc, perceivedPath); ok {
		perceived = asString(v)
	}
	var actions []model.Action
	if actionsPath != "" {
		if v, ok := lookup(doc, actionsPath); ok {
			if list, isList := v.([]any); isList {
				for _, item := range list {
					if m, isMap := item.(map[string]any); isMap {
						a := model.Action{Arguments: map[string]any{}}
						if n, ok := m["name"].(string); ok {
							a.Name = n
						} else if n, ok := m["tool"].(string); ok {
							a.Name = n
						}
						if args, ok := m["arguments"].(map[string]any); ok {
							a.Arguments = args
						} else if args, ok := m["args"].(map[string]any); ok {
							a.Arguments = args
						}
						a.Result, _ = m["result"].(string)
						a.Error, _ = m["error"].(string)
						a.Blocked, _ = m["blocked"].(bool)
						if a.Name != "" {
							actions = append(actions, a)
						}
					}
				}
			}
		}
	}
	return text, actions, perceived
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	enc, _ := json.Marshal(v)
	return string(enc)
}

func lookup(doc any, path string) (any, bool) {
	cur := doc
	for _, part := range strings.Split(path, ".") {
		switch node := cur.(type) {
		case map[string]any:
			v, ok := node[part]
			if !ok {
				return nil, false
			}
			cur = v
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(node) {
				return nil, false
			}
			cur = node[i]
		default:
			return nil, false
		}
	}
	return cur, true
}
