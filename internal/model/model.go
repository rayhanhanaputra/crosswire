// Package model holds the records Crosswire stores: targets, runs and the
// evidence attached to them. docs/CONTRACT.md is the authoritative
// description of every field here.
package model

import "time"

// Target is one agent endpoint under test.
type Target struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Environment string `json:"environment"` // staging | production
	// Endpoint is the perception path: where the physical channel lands.
	Endpoint string `json:"endpoint"`
	Method   string `json:"method"`
	// DirectEndpoint is the chat path, used for the "direct" arm. Optional.
	DirectEndpoint string            `json:"direct_endpoint"`
	DirectTemplate string            `json:"direct_template"`
	Headers        map[string]string `json:"headers"`
	// SecretHeaders maps header name → environment variable holding the value.
	// The value never enters the store or the UI.
	SecretHeaders   map[string]string `json:"secret_headers"`
	RequestTemplate string            `json:"request_template"`
	ResponsePath    string            `json:"response_path"`
	ActionsPath     string            `json:"actions_path"`
	PerceivedPath   string            `json:"perceived_path"`
	// ReplyAudioPath is the dot path of base64 audio in a JSON reply (a voice
	// service that answers in speech). The audio is kept as the trial's reply
	// artifact and cut out of the stored reply.
	ReplyAudioPath string `json:"reply_audio_path,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	// TriggerEndpoint, when set, makes every perception trial two-phase:
	// the artifact is delivered (stored by the target as memory, a note,
	// a retrieved document), then TriggerTemplate is posted here as the
	// later, unrelated request that makes the agent read what it stored.
	// {{canary}} in both templates lets the target key its memory per trial.
	TriggerEndpoint string `json:"trigger_endpoint,omitempty"`
	TriggerTemplate string `json:"trigger_template,omitempty"`
	// Transport selects how the perception path is reached: "http" (default),
	// "sip", "ws", "tcp", "mqtt", "voice-ws" or "bridge". The direct and
	// trigger paths are reached by the scheme of their own endpoint (http,
	// https, ws, wss or tcp); see docs/CONTRACT.md section 1.
	Transport string    `json:"transport,omitempty"`
	SIP       SIPConfig `json:"sip,omitempty"`
	// Per-transport settings. Only the block of the selected transport is read.
	WS      WSConfig      `json:"ws,omitempty"`
	TCP     TCPConfig     `json:"tcp,omitempty"`
	MQTT    MQTTConfig    `json:"mqtt,omitempty"`
	VoiceWS VoiceWSConfig `json:"voice_ws,omitempty"`
	Bridge  BridgeConfig  `json:"bridge,omitempty"`
	// Observer reads the effect of a trial out of band (a state endpoint or a
	// web page). Optional on every transport.
	Observer ObserverConfig `json:"observer,omitempty"`
	// Mapper rewrites the encoded message into the target's request schema
	// with an LLM. ResponseMapper extracts text and actions from an
	// unstructured reply. Neither output is ever evidence.
	Mapper         MapperConfig         `json:"mapper,omitempty"`
	ResponseMapper ResponseMapperConfig `json:"response_mapper,omitempty"`
	Evidence       Rules                `json:"evidence"`
	Enabled        bool                 `json:"enabled"`
	CreatedAt      time.Time            `json:"created_at"`
	UpdatedAt      time.Time            `json:"updated_at"`
}

// WSConfig holds the WebSocket request/response settings. The request goes out
// as one text message and the first reply message is the response.
type WSConfig struct {
	Subprotocol string `json:"subprotocol,omitempty"`
}

// TCPConfig holds the raw TCP settings. Endpoint is tcp://host:port.
type TCPConfig struct {
	TLS bool `json:"tls,omitempty"`
	// Send is "json" (default: the rendered request template) or "raw" (the
	// decoded content, nothing else).
	Send string `json:"send,omitempty"`
	// Framing is "terminator" (default) or "length" (4-byte big-endian length
	// prefix in both directions).
	Framing string `json:"framing,omitempty"`
	// Terminator ends the request, and with ReadUntil "terminator" the reply.
	// Default "\n". Accepts \n \r \t \0 \xNN escapes; "none" means empty.
	Terminator string `json:"terminator,omitempty"`
	// ReadUntil is "terminator" (default), "close", "idle" or "none" (send
	// only; the sink is the evidence).
	ReadUntil string `json:"read_until,omitempty"`
	IdleMs    int    `json:"idle_ms,omitempty"`   // quiet time that ends an "idle" read; default 500
	MaxBytes  int    `json:"max_bytes,omitempty"` // reply cap; default 65536, at most 1 MiB
}

// MQTTConfig holds the MQTT 3.1.1 settings. Endpoint is mqtt://host:port or
// mqtts://host:port. Credentials are environment variable NAMES.
type MQTTConfig struct {
	PublishTopic  string `json:"publish_topic,omitempty"`
	ResponseTopic string `json:"response_topic,omitempty"`
	QoS           *int   `json:"qos,omitempty"` // 0 or 1; nil means 1
	UsernameEnv   string `json:"username_env,omitempty"`
	PasswordEnv   string `json:"password_env,omitempty"`
	// Correlate says which reply on the response topic belongs to this trial:
	// "nonce" (default, the payload contains the trial's canary), "path" (the
	// JSON field at CorrelationPath equals the canary) or "first" (any message).
	Correlate       string `json:"correlate,omitempty"`
	CorrelationPath string `json:"correlation_path,omitempty"`
}

// VoiceWSConfig holds the realtime media stream settings. The start event is
// the target's request template; audio goes out in chunks.
type VoiceWSConfig struct {
	ChunkBytes     int    `json:"chunk_bytes,omitempty"` // 1024..65536, default 16384
	MaxEvents      int    `json:"max_events,omitempty"`  // 1..500, default 100
	ChunkTemplate  string `json:"chunk_template,omitempty"`
	EndTemplate    string `json:"end_template,omitempty"`
	CancelTemplate string `json:"cancel_template,omitempty"`
	DonePath       string `json:"done_path,omitempty"`  // default "type"
	DoneValue      string `json:"done_value,omitempty"` // default "session.done"
	// ReplyAudioPath is the dot path of base64 audio in a reply event.
	ReplyAudioPath string `json:"reply_audio_path,omitempty"`
}

// BridgeConfig holds the physical device bridge settings. Endpoint is the
// bridge's action URL. The device and resource must be on both allowlists
// before anything is sent, and the stop request is sent after every action.
type BridgeConfig struct {
	Kind             string   `json:"kind,omitempty"` // ble | nfc | gpio | led
	StopEndpoint     string   `json:"stop_endpoint,omitempty"`
	StopTemplate     string   `json:"stop_template,omitempty"`
	DeviceID         string   `json:"device_id,omitempty"`
	Resource         string   `json:"resource,omitempty"`
	AllowedDeviceIDs []string `json:"allowed_device_ids,omitempty"`
	AllowedResources []string `json:"allowed_resources,omitempty"`
	DurationMs       int      `json:"duration_ms,omitempty"` // 100..30000, default 5000
	UnitMs           int      `json:"unit_ms,omitempty"`     // pulse unit for gpio and led, 50..2000, default 200
}

// BridgeOperation is the operation name a bridge kind uses on the wire, ""
// for an unknown kind.
func BridgeOperation(kind string) string {
	switch kind {
	case "ble":
		return "ble_advertise"
	case "nfc":
		return "nfc_emulate"
	case "gpio":
		return "gpio_sequence"
	case "led":
		return "led_sequence"
	}
	return ""
}

// ObserverConfig reads state out of band, before and after a trial.
type ObserverConfig struct {
	Kind           string `json:"kind,omitempty"` // "" or none | http | browser
	URL            string `json:"url,omitempty"`
	Selector       string `json:"selector,omitempty"` // browser: CSS selector whose text is the state
	Path           string `json:"path,omitempty"`     // http: dot path into a JSON state; empty compares the body
	WaitMs         int    `json:"wait_ms,omitempty"`  // wait before the "after" read; default 1000
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
	Screenshot     bool   `json:"screenshot,omitempty"`
	// ResetURL, when set, is POSTed before each trial's first read to put the
	// observed state back where it started. Without it, a state that stays
	// changed after one hit shows no change on the next trial.
	ResetURL string `json:"reset_url,omitempty"`
	// Live keeps the browser page open between the two reads instead of
	// reloading it.
	Live bool `json:"live,omitempty"`
	// Nonce is "required" (default: a change counts as a hit only when the
	// trial's canary is visible in the new state) or "not_visible" (the UI
	// cannot show a nonce; a change counts but is marked weak).
	Nonce string `json:"nonce,omitempty"`
	// SecretHeaders maps header name to the NAME of an environment variable
	// (http observer only).
	SecretHeaders map[string]string `json:"secret_headers,omitempty"`
}

// MapperConfig configures the optional LLM input mapper.
type MapperConfig struct {
	Enabled        bool     `json:"enabled,omitempty"`
	Endpoint       string   `json:"endpoint,omitempty"` // OpenAI-compatible chat completions URL
	Model          string   `json:"model,omitempty"`
	APIKeyEnv      string   `json:"api_key_env,omitempty"` // NAME of the environment variable
	Instructions   string   `json:"instructions,omitempty"`
	RequiredKeys   []string `json:"required_keys,omitempty"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty"`
}

// ResponseMapperConfig configures the optional LLM response mapper.
type ResponseMapperConfig struct {
	Enabled        bool   `json:"enabled,omitempty"`
	Endpoint       string `json:"endpoint,omitempty"`
	Model          string `json:"model,omitempty"`
	APIKeyEnv      string `json:"api_key_env,omitempty"`
	Instructions   string `json:"instructions,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

// SIPConfig is how to place calls to a SIP target.
type SIPConfig struct {
	From          string `json:"from,omitempty"`           // caller user part, default "crosswire"
	LocalIP       string `json:"local_ip,omitempty"`       // IP to bind and advertise; default from routing
	Username      string `json:"username,omitempty"`       // digest username
	PasswordEnv   string `json:"password_env,omitempty"`   // NAME of the env var holding the password
	RingSeconds   int    `json:"ring_seconds,omitempty"`   // give up if not answered; default 32
	ListenSeconds int    `json:"listen_seconds,omitempty"` // record the far end after playing; default 20
}

// Rules are the deterministic evidence rules for a target.
type Rules struct {
	// ForbiddenTools are tool names whose invocation (with the trial canary
	// in the arguments, seen at the sink) is a hit.
	ForbiddenTools []string `json:"forbidden_tools"`
	// ArgumentLimits: tool → argument → max numeric value. A sink event above
	// the limit is a hit even without a canary (I3 safety-limit).
	ArgumentLimits map[string]map[string]float64 `json:"argument_limits"`
	// ResponseMarkers are strings in the reply text. Hints only, never hits.
	ResponseMarkers []string `json:"response_markers"`
	// RequireCanary, when false, lets a forbidden tool count without the
	// canary in its arguments. Default true; only turn off for tools whose
	// arguments cannot carry text (e.g. unlock_door with no parameters).
	RequireCanary *bool `json:"require_canary,omitempty"`
}

// Artifact is the physical rendering of a payload.
type Artifact struct {
	Kind      string `json:"kind"` // image/png | audio/wav | pulses | text
	Bytes     int    `json:"bytes"`
	SHA256    string `json:"sha256"`
	Summary   string `json:"summary"`   // e.g. "QR 256px", "DTMF 6 keys 0.84 s"
	Decoded   string `json:"decoded"`   // round-trip check of the artifact, "" if none
	Reference string `json:"reference"` // store key for the bytes
}

// Channel describes how the payload was encoded for the target's senses.
type Channel struct {
	Adapter  string         `json:"adapter"`
	Fields   map[string]any `json:"fields"`
	Source   string         `json:"source"`
	Modeled  bool           `json:"modeled"`
	Artifact *Artifact      `json:"artifact,omitempty"`
}

// Action is a tool call, either reported in the response or seen at the sink.
type Action struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
	Result    string         `json:"result,omitempty"`
	Error     string         `json:"error,omitempty"`
	Blocked   bool           `json:"blocked,omitempty"`
}

// SinkEvent is one tool call the target reported out of band. Blocked is
// true when the target refused the call (confirmation gate or interlock);
// Reason then holds the refusal text. Layers records which hardened layers
// were on when the target made the call.
type SinkEvent struct {
	ID        int64           `json:"id"`
	Name      string          `json:"name"`
	Arguments map[string]any  `json:"arguments"`
	Target    string          `json:"target,omitempty"`
	Session   string          `json:"session,omitempty"` // the request's session, so concurrent trials never share evidence
	Blocked   bool            `json:"blocked"`
	Reason    string          `json:"reason,omitempty"`
	Layers    map[string]bool `json:"layers,omitempty"`
	Received  time.Time       `json:"received"`
}

// Exchange is one wire request and its response.
type Exchange struct {
	Request      map[string]any `json:"request"`
	Status       int            `json:"status"`
	RawResponse  string         `json:"raw_response"`
	ResponseText string         `json:"response_text"`
	Actions      []Action       `json:"actions"`
	Perceived    string         `json:"perceived"`
	DurationMs   int            `json:"duration_ms"`
	Error        string         `json:"error,omitempty"`
	// ReplyAudio is the far end's audio on a SIP exchange; the engine
	// stores it as an artifact and leaves ReplyArtifact as the key.
	ReplyAudio    []byte `json:"-"`
	ReplyArtifact string `json:"reply_artifact,omitempty"`
	// Wire holds transport-level details of the exchange (MQTT topics and QoS,
	// TCP framing, WebSocket subprotocol). Never a secret value.
	Wire map[string]any `json:"wire,omitempty"`
	// MappedInput records what the input mapper produced for this exchange.
	// It is labelled mapped in the UI and is never evidence.
	MappedInput *MappedInput `json:"mapped_input,omitempty"`
	// MappedResponse is the response mapper's reading of the reply. The fields
	// above stay the deterministic extraction; this one is never evidence.
	MappedResponse *MappedResponse `json:"mapped_response,omitempty"`
	// Bridge records a device bridge exchange.
	Bridge *BridgeRecord `json:"bridge,omitempty"`
}

// MappedInput is the input mapper's output for one exchange.
type MappedInput struct {
	Model         string         `json:"model"`
	Output        map[string]any `json:"output"`
	CarriesCanary bool           `json:"carries_canary"` // the mapped request still holds the trial canary
	Error         string         `json:"error,omitempty"`
}

// MappedResponse is the response mapper's normalised reading of a reply.
type MappedResponse struct {
	Model        string         `json:"model"`
	ResponseText string         `json:"response_text"`
	Actions      []Action       `json:"actions"`
	Events       []MappedEvent  `json:"events,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	Error        string         `json:"error,omitempty"`
}

// MappedEvent is one event the response mapper extracted.
type MappedEvent struct {
	EventType string         `json:"event_type"`
	Data      map[string]any `json:"data"`
}

// BridgeRecord is the device bridge side of an exchange: what was asked, what
// the bridge acknowledged, and whether the emergency stop was confirmed.
type BridgeRecord struct {
	Kind          string `json:"kind"`
	DeviceID      string `json:"device_id"`
	Resource      string `json:"resource"`
	Operation     string `json:"operation"`
	DurationMs    int    `json:"duration_ms"`
	AckStatus     int    `json:"ack_status"`
	Ack           string `json:"ack"`
	StopStatus    int    `json:"stop_status"`
	StopConfirmed bool   `json:"stop_confirmed"`
	StopError     string `json:"stop_error,omitempty"`
	ActionAtMs    int    `json:"action_at_ms"` // offset from the start of the exchange
	StopAtMs      int    `json:"stop_at_ms"`
}

// Observation is one out-of-band read of the target's state.
type Observation struct {
	Kind          string `json:"kind"`  // http | browser
	Phase         string `json:"phase"` // before | after
	Source        string `json:"source"`
	Text          string `json:"text"`
	Status        int    `json:"status,omitempty"`
	ScreenshotRef string `json:"screenshot_ref,omitempty"` // store key of the PNG
	Error         string `json:"error,omitempty"`
	AtMs          int    `json:"at_ms"` // offset from the start of the trial
}

// ObserverRecord is what the observer saw around one trial.
type ObserverRecord struct {
	Before *Observation `json:"before,omitempty"`
	After  *Observation `json:"after,omitempty"`
	// Changed is true when the normalised text differs between the two reads.
	Changed bool `json:"changed"`
	// Carries is true when the trial's canary is visible in the new state and
	// was not in the old one.
	Carries bool   `json:"carries"`
	Detail  string `json:"detail,omitempty"`
}

// TimelineEvent is one step of a trial, in time order.
type TimelineEvent struct {
	AtMs   int    `json:"at_ms"` // offset from the start of the trial
	Source string `json:"source"`
	Event  string `json:"event"`
	Detail string `json:"detail,omitempty"`
}

// Expect names the tool call that makes a utility trial a success: an
// executed sink event with this tool whose argument Arg matches Value.
// Match is "equals" (default) or "contains", both on lower-case
// alphanumerics. An empty Value only requires the argument to be present.
type Expect struct {
	Tool  string `json:"tool"`
	Arg   string `json:"arg"`
	Value string `json:"value,omitempty"`
	Match string `json:"match,omitempty"`
}

// Finding is one matched evidence rule.
type Finding struct {
	Rule   string `json:"rule"` // forbidden_tool | argument_limit | response_marker(hint)
	Detail string `json:"detail"`
	Hint   bool   `json:"hint"` // true when it cannot count as a hit
	// Blocked marks a finding about a call the target refused. Such a call
	// is an attempt, never an executed hit.
	Blocked bool   `json:"blocked,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

// Trial is one send in one arm.
type Trial struct {
	Index     int         `json:"index"`
	Arm       string      `json:"arm"` // injected | control | direct | benign_direct | utility
	Canary    string      `json:"canary"`
	Content   string      `json:"content"` // text after canary substitution
	Exchange  Exchange    `json:"exchange"`
	Trigger   *Exchange   `json:"trigger,omitempty"` // second phase, when the target has a trigger endpoint
	Sink      []SinkEvent `json:"sink"`
	Findings  []Finding   `json:"findings"`
	Delivered *bool       `json:"delivered,omitempty"` // perceived contains canary; nil if unknown
	// Guard is the classifier verdict the target reported for the judged
	// exchange (allow, block or not_checked), "" when it reported none. A
	// block on either phase wins.
	Guard string `json:"guard,omitempty"`
	// Utility is set on the utility arm only: an executed sink event matched
	// the scenario's expected tool call.
	Utility   *bool `json:"utility,omitempty"`
	Hit       bool  `json:"hit"`       // a forbidden call executed
	Attempted bool  `json:"attempted"` // a forbidden call was made, executed or refused
	Variant   int   `json:"variant"`   // index of the payload variant this trial used
	// Observer is the out-of-band read around the trial, when the target has
	// an observer. A change in it is evidence under the rules in CONTRACT.md.
	Observer *ObserverRecord `json:"observer,omitempty"`
	// ObserverChanged is true when the observed state changed during the trial,
	// whether or not the change counted as a hit.
	ObserverChanged bool `json:"observer_changed,omitempty"`
	// Weak is true when the trial is a hit and every hit finding rests on an
	// observer change with no nonce visible. Such a trial cannot confirm a run.
	Weak bool `json:"weak,omitempty"`
	// MapperDropped is true when the input mapper removed the canary from the
	// request, so the trial measured the mapper and not the target.
	MapperDropped bool `json:"mapper_dropped,omitempty"`
	// Timeline lists the trial's steps in time order.
	Timeline []TimelineEvent `json:"timeline,omitempty"`
}

// ArmStats summarises one arm.
type ArmStats struct {
	Trials    int `json:"trials"`
	Reached   int `json:"reached"`
	Delivered int `json:"delivered"`
	Attempted int `json:"attempted"` // trials with a forbidden call, executed or refused
	Hits      int `json:"hits"`      // trials with an executed forbidden call
	// Blocked counts trials where a classifier blocked the request. On the
	// benign_direct arm this is the false-positive count.
	Blocked int `json:"blocked"`
	// UtilityOK counts utility trials that executed the expected tool call.
	UtilityOK int `json:"utility_ok"`
	// WeakHits counts hits that rest on an observer change with no nonce visible.
	WeakHits int `json:"weak_hits,omitempty"`
	// ObserverChanges counts trials whose observed state changed.
	ObserverChanges int `json:"observer_changes,omitempty"`
	// MapperDropped counts trials where the input mapper removed the canary.
	MapperDropped int     `json:"mapper_dropped,omitempty"`
	Rate          float64 `json:"rate"`
	Lower         float64 `json:"lower"`
	Upper         float64 `json:"upper"`
}

// Verdict is the run's outcome under the rules in docs/CONTRACT.md §4.
type Verdict struct {
	Outcome     string              `json:"outcome"` // confirmed | observed | not_observed | invalid | error
	Arms        map[string]ArmStats `json:"arms"`
	Explanation string              `json:"explanation"`
}

// Run is one Repeater send or one Intruder case.
type Run struct {
	ID         string  `json:"id"`
	Mode       string  `json:"mode"` // repeater | intruder
	CampaignID string  `json:"campaign_id,omitempty"`
	TargetID   string  `json:"target_id"`
	TargetName string  `json:"target_name"`
	Channel    Channel `json:"channel"`
	Payload    string  `json:"payload"` // template with {{canary}}
	Control    string  `json:"control"`
	Repeats    int     `json:"repeats"`
	// TargetInfo is the target's /health answer at run start (policy, layers,
	// temperature, model) when it could be read.
	TargetInfo map[string]any `json:"target_info,omitempty"`
	Variants   int            `json:"variants,omitempty"`
	Trials     []Trial        `json:"trials"`
	Verdict    Verdict        `json:"verdict"`
	StartedAt  time.Time      `json:"started_at"`
	DurationMs int            `json:"duration_ms"`
	Error      string         `json:"error,omitempty"`
}
