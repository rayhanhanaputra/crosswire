// Package engine runs trials. One Run is: N injected trials through the
// physical channel, N benign controls through the same channel, and
// optionally N direct trials through the chat path, each with its own
// canary, judged by what reaches the sink. See docs/CONTRACT.md §4.
package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/channel"
	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/connector"
	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/evidence"
	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/model"
	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/sink"
	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/store"
)

const (
	maxRepeats     = 50
	maxConcurrency = 4
	sinkGrace      = 250 * time.Millisecond
)

// Request describes one run.
type Request struct {
	TargetID string `json:"target_id"`
	Mode     string `json:"mode"` // repeater | intruder
	Adapter  string `json:"adapter"`
	// Fields are the attack payload fields; string values may contain
	// {{canary}}.
	Fields map[string]any `json:"fields"`
	// ControlFields are the benign control, same adapter. Nil skips the
	// control arm (the verdict can then be at most "observed").
	ControlFields map[string]any `json:"control_fields"`
	// Direct also sends the decoded payload text through the chat path.
	Direct      bool `json:"direct"`
	Repeats     int  `json:"repeats"`
	Concurrency int  `json:"concurrency"`
	// ProductionAck must be true for production targets.
	ProductionAck bool   `json:"production_acknowledged"`
	CampaignID    string `json:"campaign_id,omitempty"`
	// RunID lets the caller choose the id up front (async API). Empty
	// means the engine generates one.
	RunID string `json:"-"`
}

// Engine wires the pieces together.
type Engine struct {
	Store   *store.Store
	Sink    *sink.Sink
	Encoder channel.Encoder
	// OnTrial is called after each trial completes, for live UI updates. It
	// is invoked from concurrent goroutines (up to Concurrency at once), so
	// it must be safe to call in parallel.
	OnTrial func(runID string, t model.Trial)
}

// Validate checks a request against the target before any send.
func (e *Engine) Validate(req Request) (model.Target, error) {
	var t model.Target
	if req.Repeats <= 0 {
		req.Repeats = 1
	}
	if req.Repeats > maxRepeats {
		return t, fmt.Errorf("repeats is capped at %d", maxRepeats)
	}
	t, err := e.Store.GetTarget(req.TargetID)
	if err != nil {
		return t, fmt.Errorf("target: %w", err)
	}
	if !t.Enabled {
		return t, errors.New("target is disabled")
	}
	if t.Environment == "production" && !req.ProductionAck {
		return t, errors.New("production targets require explicit acknowledgement")
	}
	if req.Fields == nil {
		return t, errors.New("fields are required")
	}
	if _, err := e.Encoder.Encode(context.Background(), req.Adapter, channel.Substitute(req.Fields, "cw-preview")); err != nil {
		return t, fmt.Errorf("payload: %w", err)
	}
	if req.ControlFields != nil {
		if _, err := e.Encoder.Encode(context.Background(), req.Adapter, channel.Substitute(req.ControlFields, "cw-preview")); err != nil {
			return t, fmt.Errorf("control: %w", err)
		}
	}
	if req.Direct && t.DirectEndpoint == "" {
		return t, errors.New("direct arm requested but the target has no direct endpoint")
	}
	if t.Transport == "sip" {
		enc, _ := e.Encoder.Encode(context.Background(), req.Adapter, channel.Substitute(req.Fields, "cw-preview"))
		if enc.Artifact == nil || enc.Artifact.Kind != "audio/wav" {
			return t, errors.New("a sip target takes audio only: use the speech or dtmf adapter")
		}
	}
	payloadText := fmt.Sprint(req.Fields)
	if bad := evidence.SelfMatching(t.Evidence, payloadText); len(bad) > 0 {
		return t, fmt.Errorf("response marker %q occurs in the payload; it cannot be evidence", bad[0])
	}
	return t, nil
}

// Run executes the request and stores the result.
func (e *Engine) Run(ctx context.Context, req Request) (model.Run, error) {
	run := model.Run{
		ID:         orDefault(req.RunID, NewID()),
		Mode:       orDefault(req.Mode, "repeater"),
		TargetID:   req.TargetID,
		StartedAt:  time.Now(),
		Repeats:    max(req.Repeats, 1),
		Payload:    fmt.Sprint(req.Fields),
		CampaignID: req.CampaignID,
	}
	t, err := e.Validate(req)
	if err != nil {
		run.Error = err.Error()
		return run, err
	}
	run.TargetName = t.Name
	if req.ControlFields != nil {
		run.Control = fmt.Sprint(req.ControlFields)
	}

	conc := req.Concurrency
	if conc <= 0 {
		conc = 1
	}
	if conc > maxConcurrency {
		conc = maxConcurrency
	}
	// Argument-limit rules and canary-free rules cannot be attributed to a
	// trial when sends overlap, so those targets run one at a time.
	if len(t.Evidence.ArgumentLimits) > 0 || (t.Evidence.RequireCanary != nil && !*t.Evidence.RequireCanary) || t.Environment == "production" {
		conc = 1
	}

	type job struct {
		index int
		arm   string
	}
	var jobs []job
	for i := 0; i < run.Repeats; i++ {
		jobs = append(jobs, job{i, "injected"})
		if req.ControlFields != nil {
			jobs = append(jobs, job{i, "control"})
		}
		if req.Direct {
			jobs = append(jobs, job{i, "direct"})
		}
	}
	trials := make([]model.Trial, len(jobs))
	var wg sync.WaitGroup
	sem := make(chan struct{}, conc)
	for i, j := range jobs {
		wg.Add(1)
		go func(slot int, j job) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			tr := e.trial(ctx, t, req, run.ID, j.index, j.arm)
			trials[slot] = tr
			if e.OnTrial != nil {
				e.OnTrial(run.ID, tr)
			}
		}(i, j)
	}
	wg.Wait()
	run.Trials = trials
	if len(trials) > 0 && trials[0].Arm == "injected" {
		run.Channel = e.channelSummary(req, run.ID, trials[0])
	}
	run.Verdict = evidence.Judge(t.Evidence, trials)
	run.DurationMs = int(time.Since(run.StartedAt).Milliseconds())
	if err := e.Store.PutRun(run); err != nil {
		return run, fmt.Errorf("store run: %w", err)
	}
	return run, nil
}

func (e *Engine) trial(ctx context.Context, t model.Target, req Request, runID string, index int, arm string) model.Trial {
	canary := "cw-" + newHex(4)
	tr := model.Trial{Index: index, Arm: arm, Canary: canary}

	var enc channel.Encoded
	var err error
	switch arm {
	case "control":
		enc, err = e.Encoder.Encode(ctx, req.Adapter, channel.Substitute(req.ControlFields, canary))
	case "direct":
		// The direct arm types the decoded payload into chat.
		var inj channel.Encoded
		inj, err = e.Encoder.Encode(ctx, req.Adapter, channel.Substitute(req.Fields, canary))
		if err == nil {
			enc, err = e.Encoder.Encode(ctx, "text", map[string]any{"text": inj.Input.Content})
		}
	default:
		enc, err = e.Encoder.Encode(ctx, req.Adapter, channel.Substitute(req.Fields, canary))
	}
	if err != nil {
		tr.Exchange.Error = "encode: " + err.Error()
		return tr
	}
	tr.Content = enc.Input.Content
	enc.Input.Canary = canary
	if enc.Bytes != nil && enc.Artifact != nil {
		key := fmt.Sprintf("%s/%s/%d", runID, arm, index)
		if err := e.Store.PutArtifact(key, enc.Bytes); err == nil {
			enc.Artifact.Reference = key
		}
	}

	cursor := e.Sink.Cursor()
	connArm := connector.Perception
	if arm == "direct" {
		connArm = connector.Direct
	}
	ex, sendErr := connector.Send(ctx, t, connArm, enc.Input)
	if sendErr != nil && ex.Error == "" {
		ex.Error = sendErr.Error()
	}
	ex.Request = redactMedia(ex.Request)
	if len(ex.ReplyAudio) > 0 {
		key := fmt.Sprintf("%s/%s/%d/reply", runID, arm, index)
		if err := e.Store.PutArtifact(key, ex.ReplyAudio); err == nil {
			ex.ReplyArtifact = key
		}
		ex.ReplyAudio = nil
	}
	tr.Exchange = ex
	// Two-phase trial: the artifact was stored; now the routine request
	// that makes the agent read it. Findings are judged on the trigger's
	// response and on every sink event since the first send.
	judged := ex
	if sendErr == nil && arm != "direct" && t.TriggerEndpoint != "" {
		tex, terr := connector.Send(ctx, t, connector.Trigger, enc.Input)
		if terr != nil && tex.Error == "" {
			tex.Error = terr.Error()
		}
		tex.Request = redactMedia(tex.Request)
		tr.Trigger = &tex
		if terr == nil {
			judged = tex
			if judged.Perceived == "" {
				judged.Perceived = ex.Perceived
			}
		} else {
			sendErr = terr
		}
	}
	if sendErr == nil {
		time.Sleep(sinkGrace)
	}
	tr.Sink = e.Sink.Since(cursor, "")
	tr.Findings = evidence.Match(t.Evidence, canary, judged, tr.Sink)
	tr.Hit = evidence.IsHit(tr.Findings)
	if judged.Perceived != "" && evidence.Carries(tr.Content, canary) {
		d := evidence.Carries(judged.Perceived, canary)
		tr.Delivered = &d
	}
	return tr
}

// channelSummary re-encodes the first injected trial so the run carries one
// representative artifact description; the bytes were stored by trial().
func (e *Engine) channelSummary(req Request, runID string, first model.Trial) model.Channel {
	enc, err := e.Encoder.Encode(context.Background(), req.Adapter, channel.Substitute(req.Fields, first.Canary))
	ch := model.Channel{Adapter: req.Adapter, Fields: req.Fields}
	if err == nil {
		ch.Source = enc.Input.Source
		ch.Modeled = enc.Modeled
		ch.Artifact = enc.Artifact
		if ch.Artifact != nil && enc.Bytes != nil {
			ch.Artifact.Reference = fmt.Sprintf("%s/injected/%d", runID, first.Index)
		}
	}
	return ch
}

// NewID returns a time-sortable run id.
func NewID() string { return time.Now().UTC().Format("20060102-150405") + "-" + newHex(3) }

func newHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

// redactMedia replaces base64 media in a stored request with a short marker.
// The bytes are already stored once as the trial's artifact; keeping them in
// every request body made a ten-trial voice run 12 MB of JSON.
func redactMedia(req map[string]any) map[string]any {
	if req == nil {
		return nil
	}
	out := make(map[string]any, len(req))
	for k, v := range req {
		s, isStr := v.(string)
		if isStr && (k == "image_base64" || k == "audio_base64" || k == "file_base64") && len(s) > 64 {
			out[k] = fmt.Sprintf("<%s omitted: %d base64 characters>", k, len(s))
			continue
		}
		if m, isMap := v.(map[string]any); isMap {
			out[k] = redactMedia(m)
			continue
		}
		out[k] = v
	}
	return out
}
