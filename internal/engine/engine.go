// Package engine runs trials. One Run is: N injected trials through the
// physical channel, N benign controls through the same channel, and
// optionally N direct trials through the chat path, each with its own
// canary, judged by what reaches the sink. See docs/CONTRACT.md §4.
package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rayhanhanaputra/crosswire/internal/channel"
	"github.com/rayhanhanaputra/crosswire/internal/connector"
	"github.com/rayhanhanaputra/crosswire/internal/evidence"
	"github.com/rayhanhanaputra/crosswire/internal/model"
	"github.com/rayhanhanaputra/crosswire/internal/observer"
	"github.com/rayhanhanaputra/crosswire/internal/sink"
	"github.com/rayhanhanaputra/crosswire/internal/store"
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
	// Variants are paraphrases of the payload. Trial i of the injected (and
	// direct) arm uses Variants[i mod len]; keys absent from a variant fall
	// back to Fields. ControlVariants does the same for the control arm.
	Variants        []map[string]any `json:"variants,omitempty"`
	ControlVariants []map[string]any `json:"control_variants,omitempty"`
	// Direct also sends the decoded payload text through the chat path.
	Direct bool `json:"direct"`
	// BenignDirect, when set, adds the benign_direct arm: this plain text
	// goes through the chat path once per repeat. The arm measures how often
	// a chat-path classifier blocks a legitimate request.
	BenignDirect string `json:"benign_direct,omitempty"`
	// Utility, when set, adds the utility arm: a legitimate task that should
	// execute a privileged tool. See Utility.
	Utility     *Utility `json:"utility,omitempty"`
	Repeats     int      `json:"repeats"`
	Concurrency int      `json:"concurrency"`
	// ProductionAck must be true for production targets.
	ProductionAck bool   `json:"production_acknowledged"`
	CampaignID    string `json:"campaign_id,omitempty"`
	// RunID lets the caller choose the id up front (async API). Empty
	// means the engine generates one.
	RunID string `json:"-"`
}

// Utility is one legitimate task per scenario. Fields, when given, go
// through Adapter (default: the request's adapter) on the perception path,
// followed by the trigger when the target has one. Message, when given, goes
// through the chat path after that. At least one of the two is required. The
// trial succeeds when an executed sink event of the trial's session matches
// Expect.
type Utility struct {
	Adapter string         `json:"adapter,omitempty"`
	Fields  map[string]any `json:"fields,omitempty"`
	Message string         `json:"message,omitempty"`
	Expect  model.Expect   `json:"expect"`
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
	if err := e.validateVariants(t, req); err != nil {
		return t, err
	}
	if req.Direct && t.DirectEndpoint == "" {
		return t, errors.New("direct arm requested but the target has no direct endpoint")
	}
	if req.BenignDirect != "" && t.DirectEndpoint == "" {
		return t, errors.New("benign_direct arm requested but the target has no direct endpoint")
	}
	if u := req.Utility; u != nil {
		if u.Expect.Tool == "" || u.Expect.Arg == "" {
			return t, errors.New("utility.expect needs a tool and an arg")
		}
		if u.Expect.Match != "" && u.Expect.Match != "equals" && u.Expect.Match != "contains" {
			return t, fmt.Errorf("utility.expect.match %q: want equals or contains", u.Expect.Match)
		}
		if len(u.Fields) == 0 && strings.TrimSpace(u.Message) == "" {
			return t, errors.New("utility needs fields, a message, or both")
		}
		if strings.TrimSpace(u.Message) != "" && t.DirectEndpoint == "" {
			return t, errors.New("utility message needs a target with a direct endpoint")
		}
		if len(u.Fields) > 0 {
			if _, err := e.Encoder.Encode(context.Background(), orDefault(u.Adapter, req.Adapter), channel.Substitute(u.Fields, "cw-preview")); err != nil {
				return t, fmt.Errorf("utility: %w", err)
			}
		}
	}
	if err := e.validateTransport(t, req); err != nil {
		return t, err
	}
	payloadText := fmt.Sprint(req.Fields)
	if bad := evidence.SelfMatching(t.Evidence, payloadText); len(bad) > 0 {
		return t, fmt.Errorf("response marker %q occurs in the payload; it cannot be evidence", bad[0])
	}
	return t, nil
}

// validateVariants checks that every variant encodes, does not carry a
// response marker, and, where the target requires the canary, renders the
// canary into the decoded content.
func (e *Engine) validateVariants(t model.Target, req Request) error {
	needCanary := t.Evidence.RequireCanary == nil || *t.Evidence.RequireCanary
	check := func(label string, base map[string]any, variants []map[string]any) error {
		for i := range variants {
			fields := mergeFields(base, variants[i])
			enc, err := e.Encoder.Encode(context.Background(), req.Adapter, channel.Substitute(fields, "cw-preview"))
			if err != nil {
				return fmt.Errorf("%s variant %d: %w", label, i, err)
			}
			if needCanary && !evidence.Carries(enc.Input.Content, "cw-preview") {
				return fmt.Errorf("%s variant %d does not render the canary, but the target requires it", label, i)
			}
			if bad := evidence.SelfMatching(t.Evidence, fmt.Sprint(fields)); len(bad) > 0 {
				return fmt.Errorf("%s variant %d: response marker %q occurs in the payload", label, i, bad[0])
			}
		}
		return nil
	}
	if err := check("payload", req.Fields, req.Variants); err != nil {
		return err
	}
	if req.ControlFields != nil {
		return check("control", req.ControlFields, req.ControlVariants)
	}
	return nil
}

// mergeFields returns base overlaid with the keys of variant.
func mergeFields(base, variant map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(variant))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range variant {
		out[k] = v
	}
	return out
}

// pickFields chooses the fields and variant index for trial i.
func pickFields(base map[string]any, variants []map[string]any, i int) (map[string]any, int) {
	if len(variants) == 0 {
		return base, 0
	}
	idx := i % len(variants)
	return mergeFields(base, variants[idx]), idx
}

// targetInfo reads the reference target's /health answer so the run records
// which layers, temperature and model were active. Best effort: any failure
// returns nil. Production targets are never probed.
func targetInfo(ctx context.Context, t model.Target) map[string]any {
	if t.Environment == "production" {
		return nil
	}
	raw := t.Endpoint
	if t.Transport == "sip" || !strings.HasPrefix(raw, "http") {
		raw = t.DirectEndpoint
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil
	}
	cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, u.Scheme+"://"+u.Host+"/health", nil)
	if err != nil {
		return nil
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var info map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&info); err != nil {
		return nil
	}
	return info
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
	rc := &runCtx{obs: observer.New(t.Observer)}
	defer rc.obs.Close()
	run.TargetInfo = targetInfo(ctx, t)
	// The name the target reports, not the store's label: sink events carry
	// the former. Empty when /health could not be read, which turns the
	// target filter off.
	reported, _ := run.TargetInfo["name"].(string)
	run.Variants = max(len(req.Variants), len(req.ControlVariants))
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
	// An observer reads one shared page or endpoint, and a bridge drives one
	// device, so those runs are serial as well.
	if len(t.Evidence.ArgumentLimits) > 0 || (t.Evidence.RequireCanary != nil && !*t.Evidence.RequireCanary) || t.Environment == "production" ||
		rc.obs != nil || t.Transport == "bridge" {
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
		if req.BenignDirect != "" {
			jobs = append(jobs, job{i, "benign_direct"})
		}
		if req.Utility != nil {
			jobs = append(jobs, job{i, "utility"})
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
			tr := e.trial(ctx, t, req, run.ID, reported, j.index, j.arm, rc)
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
	run.Verdict = evidence.JudgeWith(t.Evidence, rc.obs != nil, trials)
	run.DurationMs = int(time.Since(run.StartedAt).Milliseconds())
	if err := e.Store.PutRun(run); err != nil {
		return run, fmt.Errorf("store run: %w", err)
	}
	return run, nil
}

// runCtx is the state one Run shares across its trials.
type runCtx struct {
	obs *observer.Observer
	// halted is set when a bridge stop was not confirmed: nothing more is sent.
	halted atomic.Bool
}

func (e *Engine) trial(ctx context.Context, t model.Target, req Request, runID, reported string, index int, arm string, rc *runCtx) (tr model.Trial) {
	began := time.Now()
	tl := &timeline{start: began}
	canary := "cw-" + newHex(4)
	tr = model.Trial{Index: index, Arm: arm, Canary: canary}
	defer func() { tr.Timeline = tl.sorted() }()
	if rc.halted.Load() {
		tr.Exchange.Error = "run halted: an earlier emergency stop was not confirmed"
		tl.add("engine", "halted", tr.Exchange.Error)
		return tr
	}
	fields, variant := pickFields(req.Fields, req.Variants, index)
	if arm == "control" {
		fields, variant = pickFields(req.ControlFields, req.ControlVariants, index)
	}
	tr.Variant = variant

	util := req.Utility
	utilPerception := arm == "utility" && len(util.Fields) > 0
	var enc channel.Encoded
	var err error
	switch arm {
	case "control":
		enc, err = e.Encoder.Encode(ctx, req.Adapter, channel.Substitute(fields, canary))
	case "direct":
		// The direct arm types the decoded payload into chat.
		var inj channel.Encoded
		inj, err = e.Encoder.Encode(ctx, req.Adapter, channel.Substitute(fields, canary))
		if err == nil {
			enc, err = e.Encoder.Encode(ctx, "text", map[string]any{"text": inj.Input.Content})
		}
	case "benign_direct":
		enc, err = e.Encoder.Encode(ctx, "text", channel.Substitute(map[string]any{"text": req.BenignDirect}, canary))
	case "utility":
		if utilPerception {
			enc, err = e.Encoder.Encode(ctx, orDefault(util.Adapter, req.Adapter), channel.Substitute(util.Fields, canary))
		} else {
			enc, err = e.Encoder.Encode(ctx, "text", channel.Substitute(map[string]any{"text": util.Message}, canary))
		}
	default:
		enc, err = e.Encoder.Encode(ctx, req.Adapter, channel.Substitute(fields, canary))
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
	tl.add("engine", "encoded", describeArtifact(req.Adapter, enc))

	first := connector.Perception
	if arm == "direct" || arm == "benign_direct" || (arm == "utility" && !utilPerception) {
		first = connector.Direct
	}
	observed := rc.obs != nil && (arm == "injected" || arm == "control" || arm == "direct")
	var before, after *model.Observation
	reading := func(phase string) *model.Observation {
		r, err := rc.obs.Read(ctx, phase)
		r.Obs.AtMs = int(time.Since(began).Milliseconds())
		if len(r.Screenshot) > 0 {
			key := fmt.Sprintf("%s/%s/%d/observer-%s", runID, arm, index, phase)
			if e.Store.PutArtifact(key, r.Screenshot) == nil {
				r.Obs.ScreenshotRef = key
			}
		}
		detail := trunc(observer.Normalize(r.Obs.Text), 100)
		if err != nil {
			detail = "failed: " + err.Error()
		}
		tl.add("observer", phase+" read", detail)
		o := r.Obs
		return &o
	}
	if observed {
		if err := rc.obs.Reset(ctx); err != nil {
			tr.Exchange.Error = err.Error()
			tl.add("observer", "reset failed", err.Error())
			return tr
		}
		if rc.obs.Enabled() && t.Observer.ResetURL != "" {
			tl.add("observer", "reset", "state put back before the first read")
		}
		if before = reading("before"); before.Error != "" {
			tr.Exchange.Error = "observer before read: " + before.Error
			tr.Observer = &model.ObserverRecord{Before: before, Detail: "the first read failed, so nothing was sent"}
			return tr
		}
	}

	// Input mapper: rewrite the encoded message into the target's own schema.
	// The mapped request is recorded as mapped and is never evidence.
	var mappedIn *model.MappedInput
	if e.mapsPath(t, first) {
		mappedIn = e.mapInput(ctx, t, &enc.Input)
		tr.Content = enc.Input.Content
		tl.add("input mapper", "mapped", mapperDetail(mappedIn))
		if mappedIn.Error != "" {
			tr.Exchange.Error = mappedIn.Error
			tr.Exchange.MappedInput = mappedIn
			return tr
		}
		tr.MapperDropped = evidence.Carries(enc.Input.Content, canary) && !mappedIn.CarriesCanary
	}

	// On a bridge the stimulus stays on while the observer waits and reads, and
	// the stop request follows. Elsewhere the second read follows the sink grace.
	holdDone := false
	if observed && t.Transport == "bridge" && first == connector.Perception {
		enc.Input.AfterAction = func(hctx context.Context) error {
			select {
			case <-time.After(rc.obs.Wait()):
			case <-hctx.Done():
				return hctx.Err()
			}
			after = reading("after")
			holdDone = true
			return nil
		}
	}

	cursor := e.Sink.Cursor()
	tl.add("connector", "sent", describeSend(t, first))
	sent := time.Now()
	ex, sendErr := connector.Send(ctx, t, first, enc.Input)
	if sendErr != nil && ex.Error == "" {
		ex.Error = sendErr.Error()
	}
	if errors.Is(sendErr, connector.ErrStopNotConfirmed) {
		rc.halted.Store(true)
	}
	ex.Request = redactMedia(ex.Request)
	ex.MappedInput = mappedIn
	e.recordBridge(tl, began, sent, ex)
	if len(ex.ReplyAudio) > 0 {
		key := fmt.Sprintf("%s/%s/%d/reply", runID, arm, index)
		if err := e.Store.PutArtifact(key, ex.ReplyAudio); err == nil {
			ex.ReplyArtifact = key
		}
		ex.ReplyAudio = nil
	}
	e.mapResponse(ctx, t, &ex, tl)
	tl.add("connector", "response", describeReply(ex))
	tr.Exchange = ex
	// Two-phase trial: the artifact was stored; now the routine request
	// that makes the agent read it. Findings are judged on the trigger's
	// response and on every sink event since the first send. A utility
	// trial with both an artifact and a chat message sends the message as
	// its second phase instead.
	judged := ex
	second, secondIn := connector.Arm(""), enc.Input
	switch {
	case utilPerception && strings.TrimSpace(util.Message) != "":
		menc, merr := e.Encoder.Encode(ctx, "text", channel.Substitute(map[string]any{"text": util.Message}, canary))
		if merr != nil {
			tr.Exchange.Error = "encode: " + merr.Error()
			return tr
		}
		menc.Input.Canary = canary
		second, secondIn = connector.Direct, menc.Input
	case first == connector.Perception && t.TriggerEndpoint != "":
		second = connector.Trigger
	}
	if sendErr == nil && second != "" {
		secondIn.AfterAction = nil
		if e.mapsPath(t, second) {
			if m := e.mapInput(ctx, t, &secondIn); m.Error != "" {
				tr.Exchange.Error = m.Error
				return tr
			}
		}
		tl.add("connector", "trigger sent", describeSend(t, second))
		tex, terr := connector.Send(ctx, t, second, secondIn)
		if terr != nil && tex.Error == "" {
			tex.Error = terr.Error()
		}
		tex.Request = redactMedia(tex.Request)
		e.mapResponse(ctx, t, &tex, tl)
		tr.Trigger = &tex
		tl.add("connector", "trigger response", describeReply(tex))
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
	if observed && !holdDone && sendErr == nil {
		select {
		case <-time.After(rc.obs.Wait()):
		case <-ctx.Done():
		}
		after = reading("after")
	}
	tr.Sink = e.Sink.Since(cursor, "", canary, reported)
	for _, ev := range tr.Sink {
		verb := "executed"
		if ev.Blocked {
			verb = "refused"
		}
		tl.addAt(ev.Received, "sink", ev.Name+" "+verb, "")
	}
	tr.Guard = guardVerdict(ex.RawResponse, tr.Trigger)
	switch arm {
	case "benign_direct":
		// A benign request has no forbidden outcome to judge.
	case "utility":
		ok := evidence.UtilityMet(util.Expect, tr.Sink)
		tr.Utility = &ok
	default:
		tr.Findings = evidence.Match(t.Evidence, canary, judged, tr.Sink)
		if observed && before != nil {
			rec := observer.Compare(before, after, canary)
			tr.Observer = &rec
			tr.ObserverChanged = rec.Changed
			if after == nil || after.Error != "" {
				tr.Findings = append(tr.Findings, model.Finding{Rule: "observer_unavailable", Hint: true, Detail: "the second observer read failed, so the observed state could not be judged"})
			}
			tr.Findings = append(tr.Findings, observer.Findings(t.Observer, rec)...)
		}
		tr.Hit = evidence.IsHit(tr.Findings)
		tr.Attempted = tr.Hit || evidence.IsAttempted(tr.Findings)
		tr.Weak = tr.Hit && evidence.OnlyUnattributed(tr.Findings)
	}
	if judged.Perceived != "" && evidence.Carries(tr.Content, canary) {
		d := evidence.Carries(judged.Perceived, canary)
		tr.Delivered = &d
	}
	tl.add("engine", "judged", fmt.Sprintf("hit %v, attempted %v", tr.Hit, tr.Attempted))
	return tr
}

// guardVerdict reads the classifier verdict a reference target reports in
// its response (guardrail.verdict). A block on either phase wins; "" means
// no phase reported a guardrail.
func guardVerdict(first string, second *model.Exchange) string {
	raws := []string{first}
	if second != nil {
		raws = append(raws, second.RawResponse)
	}
	out := ""
	for _, raw := range raws {
		var d struct {
			Guardrail struct {
				Enabled bool   `json:"enabled"`
				Verdict string `json:"verdict"`
			} `json:"guardrail"`
		}
		if json.Unmarshal([]byte(raw), &d) != nil || !d.Guardrail.Enabled || d.Guardrail.Verdict == "" {
			continue
		}
		if d.Guardrail.Verdict == "block" {
			return "block"
		}
		if out == "" || out == "not_checked" {
			out = d.Guardrail.Verdict
		}
	}
	return out
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
