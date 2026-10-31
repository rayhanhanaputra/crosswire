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
	TimeoutSeconds  int               `json:"timeout_seconds"`
	// Transport is "http" (default) or "sip". With "sip", Endpoint is a
	// sip: URI and the perception arm places a call carrying the audio
	// artifact; the chat path stays HTTP.
	Transport string    `json:"transport,omitempty"`
	SIP       SIPConfig `json:"sip,omitempty"`
	Evidence  Rules     `json:"evidence"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
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
}

// SinkEvent is one tool execution the target reported out of band.
type SinkEvent struct {
	ID        int64          `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
	Target    string         `json:"target,omitempty"`
	Received  time.Time      `json:"received"`
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
}

// Finding is one matched evidence rule.
type Finding struct {
	Rule   string `json:"rule"` // forbidden_tool | argument_limit | response_marker(hint)
	Detail string `json:"detail"`
	Hint   bool   `json:"hint"` // true when it cannot count as a hit
}

// Trial is one send in one arm.
type Trial struct {
	Index     int         `json:"index"`
	Arm       string      `json:"arm"` // injected | control | direct
	Canary    string      `json:"canary"`
	Content   string      `json:"content"` // text after canary substitution
	Exchange  Exchange    `json:"exchange"`
	Sink      []SinkEvent `json:"sink"`
	Findings  []Finding   `json:"findings"`
	Delivered *bool       `json:"delivered,omitempty"` // perceived contains canary; nil if unknown
	Hit       bool        `json:"hit"`
}

// ArmStats summarises one arm.
type ArmStats struct {
	Trials    int     `json:"trials"`
	Reached   int     `json:"reached"`
	Delivered int     `json:"delivered"`
	Hits      int     `json:"hits"`
	Rate      float64 `json:"rate"`
	Lower     float64 `json:"lower"`
	Upper     float64 `json:"upper"`
}

// Verdict is the run's outcome under the rules in docs/CONTRACT.md §4.
type Verdict struct {
	Outcome     string              `json:"outcome"` // confirmed | observed | not_observed | invalid | error
	Arms        map[string]ArmStats `json:"arms"`
	Explanation string              `json:"explanation"`
}

// Run is one Repeater send or one Intruder case.
type Run struct {
	ID         string    `json:"id"`
	Mode       string    `json:"mode"` // repeater | intruder
	CampaignID string    `json:"campaign_id,omitempty"`
	TargetID   string    `json:"target_id"`
	TargetName string    `json:"target_name"`
	Channel    Channel   `json:"channel"`
	Payload    string    `json:"payload"` // template with {{canary}}
	Control    string    `json:"control"`
	Repeats    int       `json:"repeats"`
	Trials     []Trial   `json:"trials"`
	Verdict    Verdict   `json:"verdict"`
	StartedAt  time.Time `json:"started_at"`
	DurationMs int       `json:"duration_ms"`
	Error      string    `json:"error,omitempty"`
}
