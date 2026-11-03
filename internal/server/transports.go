package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"

	"github.com/rayhanhanaputra/crosswire/internal/connector"
	"github.com/rayhanhanaputra/crosswire/internal/guard"
	"github.com/rayhanhanaputra/crosswire/internal/mapper"
	"github.com/rayhanhanaputra/crosswire/internal/model"
)

var (
	keyNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{0,127}$`)
)

// Transports lists the values Target.Transport accepts, for the UI and docs.
var Transports = []string{"http", "ws", "tcp", "mqtt", "voice-ws", "sip", "bridge"}

// normalizeTransport validates the perception transport and the block of
// settings that belongs to it, and applies defaults. Settings of transports
// that are not selected are cleared, so a stored target says what it does.
func normalizeTransport(t *model.Target) error {
	t.Transport = strings.ToLower(strings.TrimSpace(t.Transport))
	if t.Transport == "http" {
		t.Transport = ""
	}
	if t.ReplyAudioPath = strings.TrimSpace(t.ReplyAudioPath); t.ReplyAudioPath != "" && !pathOK(t.ReplyAudioPath) {
		return errors.New("reply_audio_path uses dot-separated field names")
	}
	sip, ws, tcp, mq, vws, br := t.SIP, t.WS, t.TCP, t.MQTT, t.VoiceWS, t.Bridge
	t.SIP, t.WS, t.TCP, t.MQTT, t.VoiceWS, t.Bridge = model.SIPConfig{}, model.WSConfig{}, model.TCPConfig{}, model.MQTTConfig{}, model.VoiceWSConfig{}, model.BridgeConfig{}
	switch t.Transport {
	case "":
		if _, err := guard.ParseURL(t.Endpoint, "http", "https"); err != nil {
			return fmt.Errorf("endpoint must be an absolute http(s) URL without credentials: %w", err)
		}
	case "sip":
		if !strings.HasPrefix(t.Endpoint, "sip:") || !strings.Contains(t.Endpoint, "@") {
			return errors.New("a sip target's endpoint must be a sip:user@host[:port] URI")
		}
		if sip.PasswordEnv != "" && !envNameRe.MatchString(sip.PasswordEnv) {
			return errors.New("sip.password_env must be the NAME of an environment variable, not the password")
		}
		if sip.LocalIP != "" && net.ParseIP(sip.LocalIP) == nil {
			return errors.New("sip.local_ip must be an IP address")
		}
		if sip.ListenSeconds < 0 || sip.ListenSeconds > 120 || sip.RingSeconds < 0 || sip.RingSeconds > 120 {
			return errors.New("sip ring_seconds and listen_seconds must be between 0 and 120")
		}
		t.SIP = sip
	case "ws":
		if _, err := guard.ParseURL(t.Endpoint, "ws", "wss"); err != nil {
			return fmt.Errorf("a ws target's endpoint must be ws:// or wss:// without credentials: %w", err)
		}
		if len(ws.Subprotocol) > 128 || strings.ContainsAny(ws.Subprotocol, " \r\n,") {
			return errors.New("ws.subprotocol is one token")
		}
		t.WS = ws
		if err := needJSONTemplate(t); err != nil {
			return err
		}
	case "voice-ws":
		if _, err := guard.ParseURL(t.Endpoint, "ws", "wss"); err != nil {
			return fmt.Errorf("a voice-ws target's endpoint must be ws:// or wss:// without credentials: %w", err)
		}
		if err := normalizeVoiceWS(&vws); err != nil {
			return err
		}
		t.VoiceWS = vws
		if err := needJSONTemplate(t); err != nil {
			return err
		}
	case "tcp":
		u, err := guard.ParseURL(t.Endpoint, "tcp")
		if err != nil || u.Port() == "" {
			return errors.New("a tcp target's endpoint must be tcp://host:port")
		}
		if err := normalizeTCP(&tcp); err != nil {
			return err
		}
		t.TCP = tcp
		if tcp.Send != "raw" {
			if err := needJSONTemplate(t); err != nil {
				return err
			}
		}
	case "mqtt":
		if _, err := guard.ParseURL(t.Endpoint, "mqtt", "mqtts"); err != nil {
			return fmt.Errorf("an mqtt target's endpoint must be mqtt:// or mqtts:// without credentials: %w", err)
		}
		if err := needJSONTemplate(t); err != nil {
			return err
		}
		if err := normalizeMQTT(&mq, t.RequestTemplate); err != nil {
			return err
		}
		t.MQTT = mq
	case "bridge":
		if err := normalizeBridge(t, &br); err != nil {
			return err
		}
		t.Bridge = br
	default:
		return fmt.Errorf("transport %q is not supported; use %s", t.Transport, strings.Join(Transports, ", "))
	}
	return nil
}

// needJSONTemplate checks that the request template renders to a JSON object
// when filled with sample values.
func needJSONTemplate(t *model.Target) error {
	if strings.TrimSpace(t.RequestTemplate) == "" {
		return nil
	}
	if _, err := connector.Render(*t, connector.Input{Source: "s", Content: "c", Canary: "cw-00000000"}); err != nil {
		return fmt.Errorf("request_template: %w", err)
	}
	return nil
}

func normalizeVoiceWS(c *model.VoiceWSConfig) error {
	if c.ChunkBytes != 0 && (c.ChunkBytes < 1024 || c.ChunkBytes > 65536) {
		return errors.New("voice_ws.chunk_bytes must be between 1024 and 65536")
	}
	if c.MaxEvents != 0 && (c.MaxEvents < 1 || c.MaxEvents > 500) {
		return errors.New("voice_ws.max_events must be between 1 and 500")
	}
	for name, tpl := range map[string]string{"chunk_template": c.ChunkTemplate, "end_template": c.EndTemplate, "cancel_template": c.CancelTemplate} {
		if tpl == "" {
			continue
		}
		if !json.Valid([]byte(strings.ReplaceAll(tpl, "{{audio_chunk_base64}}", "QUJD"))) {
			return fmt.Errorf("voice_ws.%s must be valid JSON", name)
		}
	}
	for name, p := range map[string]string{"done_path": c.DonePath, "reply_audio_path": c.ReplyAudioPath} {
		if p != "" && !pathOK(p) {
			return fmt.Errorf("voice_ws.%s uses dot-separated field names", name)
		}
	}
	return nil
}

func pathOK(p string) bool {
	for _, part := range strings.Split(p, ".") {
		if !keyNameRe.MatchString(part) {
			return false
		}
	}
	return true
}

func normalizeTCP(c *model.TCPConfig) error {
	switch c.Send {
	case "", "json", "raw":
	default:
		return errors.New(`tcp.send must be "json" or "raw"`)
	}
	switch c.Framing {
	case "", "terminator", "length":
	default:
		return errors.New(`tcp.framing must be "terminator" or "length"`)
	}
	switch c.ReadUntil {
	case "", "terminator", "close", "idle", "none":
	default:
		return errors.New(`tcp.read_until must be "terminator", "close", "idle" or "none"`)
	}
	if _, err := connector.ParseTerminator(c.Terminator); err != nil {
		return fmt.Errorf("tcp.terminator: %w", err)
	}
	if c.IdleMs < 0 || c.IdleMs > 60000 {
		return errors.New("tcp.idle_ms must be between 0 and 60000")
	}
	if c.MaxBytes < 0 || c.MaxBytes > 1<<20 {
		return errors.New("tcp.max_bytes must be between 0 and 1048576")
	}
	return nil
}

func normalizeMQTT(c *model.MQTTConfig, template string) error {
	c.PublishTopic, c.ResponseTopic = strings.TrimSpace(c.PublishTopic), strings.TrimSpace(c.ResponseTopic)
	for label, topic := range map[string]string{"publish_topic": c.PublishTopic, "response_topic": c.ResponseTopic} {
		if err := connector.CheckMQTTTopic(strings.ReplaceAll(topic, "{{canary}}", "cw-00000000")); err != nil {
			return fmt.Errorf("mqtt.%s: %w", label, err)
		}
	}
	if c.QoS != nil && *c.QoS != 0 && *c.QoS != 1 {
		return errors.New("mqtt.qos must be 0 or 1")
	}
	for label, env := range map[string]string{"username_env": c.UsernameEnv, "password_env": c.PasswordEnv} {
		if env != "" && !envNameRe.MatchString(env) {
			return fmt.Errorf("mqtt.%s must be the NAME of an environment variable, not the credential", label)
		}
	}
	if c.PasswordEnv != "" && c.UsernameEnv == "" {
		return errors.New("an mqtt password needs a username")
	}
	if c.Correlate == "" {
		c.Correlate = "first"
		if strings.Contains(template, "{{canary}}") || strings.Contains(c.ResponseTopic, "{{canary}}") {
			c.Correlate = "nonce"
		}
	}
	switch c.Correlate {
	case "first":
	case "nonce", "path":
		if !strings.Contains(template, "{{canary}}") && !strings.Contains(c.ResponseTopic, "{{canary}}") {
			return errors.New("mqtt correlation by nonce or path needs {{canary}} in the request template (for example a correlation_id field), so the reply can be matched to its trial")
		}
		if c.Correlate == "path" && !pathOK(c.CorrelationPath) {
			return errors.New("mqtt.correlation_path is the dot path of the field that holds the canary")
		}
	default:
		return errors.New(`mqtt.correlate must be "nonce", "path" or "first"`)
	}
	return nil
}

func normalizeBridge(t *model.Target, c *model.BridgeConfig) error {
	c.Kind = strings.ToLower(strings.TrimSpace(c.Kind))
	c.StopEndpoint = strings.TrimSpace(c.StopEndpoint)
	if c.DurationMs == 0 {
		c.DurationMs = 5000
	}
	if err := connector.CheckBridge(t.Endpoint, *c); err != nil {
		return err
	}
	for name, tpl := range map[string]string{"request_template": t.RequestTemplate, "stop_template": c.StopTemplate} {
		if strings.TrimSpace(tpl) == "" {
			continue
		}
		sample := strings.NewReplacer("{{device_id}}", "d", "{{operation}}", "o", "{{resource}}", "r", "{{duration_ms}}", "1", "{{unit_ms}}", "1", "{{blink_units}}", "[]", "{{artifact_hex}}", "00", "{{content}}", "c", "{{canary}}", "cw-00000000").Replace(tpl)
		if !json.Valid([]byte(sample)) {
			return fmt.Errorf("bridge %s must render to valid JSON", name)
		}
	}
	return nil
}

func normalizeObserver(t *model.Target) error {
	o := &t.Observer
	o.Kind = strings.ToLower(strings.TrimSpace(o.Kind))
	if o.Kind == "none" {
		o.Kind = ""
	}
	if o.Kind == "" {
		*o = model.ObserverConfig{}
		return nil
	}
	if o.Kind != "http" && o.Kind != "browser" {
		return errors.New(`observer.kind must be "none", "http" or "browser"`)
	}
	u, err := guard.ParseURL(o.URL, "http", "https")
	if err != nil {
		return fmt.Errorf("observer.url: %w", err)
	}
	if u.Scheme == "http" && !guard.IsLoopbackHost(u.Hostname()) {
		return errors.New("an observer that is not on this machine must use HTTPS")
	}
	if o.Kind == "browser" && strings.TrimSpace(o.Selector) == "" {
		return errors.New("a browser observer needs a CSS selector for the element that shows the state")
	}
	if o.ResetURL != "" {
		ru, err := guard.ParseURL(o.ResetURL, "http", "https")
		if err != nil {
			return fmt.Errorf("observer.reset_url: %w", err)
		}
		if ru.Scheme == "http" && !guard.IsLoopbackHost(ru.Hostname()) {
			return errors.New("an observer reset URL that is not on this machine must use HTTPS")
		}
	}
	if o.Kind == "http" && o.Path != "" && !pathOK(o.Path) {
		return errors.New("observer.path uses dot-separated field names")
	}
	if o.WaitMs < 0 || o.WaitMs > 30000 {
		return errors.New("observer.wait_ms must be between 0 and 30000")
	}
	if o.TimeoutSeconds < 0 || o.TimeoutSeconds > 30 {
		return errors.New("observer.timeout_seconds must be between 0 and 30")
	}
	switch o.Nonce {
	case "", "required", "not_visible":
	default:
		return errors.New(`observer.nonce must be "required" or "not_visible"`)
	}
	for h, env := range o.SecretHeaders {
		if err := guard.CheckSecretHeaderName(h); err != nil {
			return err
		}
		if !envNameRe.MatchString(env) {
			return fmt.Errorf("observer.secret_headers[%s] must be the NAME of an environment variable", h)
		}
	}
	if o.Kind != "http" {
		o.SecretHeaders = nil
	}
	return nil
}

func normalizeMappers(t *model.Target) error {
	if m := &t.Mapper; m.Enabled {
		if m.Endpoint == "" || m.Model == "" || m.APIKeyEnv == "" {
			return errors.New("the input mapper needs an endpoint, a model and the NAME of the API key environment variable")
		}
		if err := mapper.CheckEndpoint(m.Endpoint); err != nil {
			return fmt.Errorf("input mapper endpoint: %w", err)
		}
		if !envNameRe.MatchString(m.APIKeyEnv) {
			return errors.New("mapper.api_key_env must be the NAME of an environment variable, not the key")
		}
		if strings.TrimSpace(m.Instructions) == "" {
			return errors.New("the input mapper needs instructions that describe the target's request schema")
		}
		if len(m.Instructions) > 8000 {
			return errors.New("mapper.instructions is limited to 8000 characters")
		}
		if !strings.Contains(t.RequestTemplate, "{{mapped}}") {
			return errors.New("with the input mapper on, the request template must contain {{mapped}}")
		}
		seen := map[string]bool{}
		for _, k := range m.RequiredKeys {
			if !keyNameRe.MatchString(k) || seen[k] {
				return errors.New("mapper.required_keys must be unique simple field names")
			}
			seen[k] = true
		}
		if m.TimeoutSeconds < 0 || m.TimeoutSeconds > 60 {
			return errors.New("mapper.timeout_seconds must be between 0 and 60")
		}
	} else {
		t.Mapper = model.MapperConfig{}
	}
	if m := &t.ResponseMapper; m.Enabled {
		if m.Endpoint == "" || m.Model == "" || m.APIKeyEnv == "" {
			return errors.New("the response mapper needs an endpoint, a model and the NAME of the API key environment variable")
		}
		if err := mapper.CheckEndpoint(m.Endpoint); err != nil {
			return fmt.Errorf("response mapper endpoint: %w", err)
		}
		if !envNameRe.MatchString(m.APIKeyEnv) {
			return errors.New("response_mapper.api_key_env must be the NAME of an environment variable, not the key")
		}
		if len(m.Instructions) > 8000 {
			return errors.New("response_mapper.instructions is limited to 8000 characters")
		}
		if m.TimeoutSeconds < 0 || m.TimeoutSeconds > 60 {
			return errors.New("response_mapper.timeout_seconds must be between 0 and 60")
		}
	} else {
		t.ResponseMapper = model.ResponseMapperConfig{}
	}
	return nil
}
