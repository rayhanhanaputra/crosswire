package server

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/rayhanhanaputra/crosswire/internal/channel"
	"github.com/rayhanhanaputra/crosswire/internal/connector"
	"github.com/rayhanhanaputra/crosswire/internal/engine"
	"github.com/rayhanhanaputra/crosswire/internal/model"
	"github.com/rayhanhanaputra/crosswire/internal/store"
)

const maxPayloads = 100

type previewRequest struct {
	TargetID string         `json:"target_id"`
	Adapter  string         `json:"adapter"`
	Fields   map[string]any `json:"fields"`
}

func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	var req previewRequest
	if !decode(w, r, &req) {
		return
	}
	const canary = "cw-preview"
	enc, err := s.enc.Encode(r.Context(), req.Adapter, channel.Substitute(req.Fields, canary))
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	enc.Input.Canary = canary
	out := map[string]any{
		"input":    map[string]string{"source": enc.Input.Source, "content": enc.Input.Content},
		"artifact": enc.Artifact,
		"modeled":  enc.Modeled,
	}
	if enc.Bytes != nil && enc.Artifact != nil {
		out["artifact_base64"] = base64.StdEncoding.EncodeToString(enc.Bytes)
		out["kind"] = enc.Artifact.Kind
	}
	if req.TargetID != "" {
		t, err := s.st.GetTarget(req.TargetID)
		if err != nil {
			storeErr(w, err)
			return
		}
		prev := enc.Input
		if t.Mapper.Enabled {
			prev.Mapped = map[string]any{"_": "produced by the input mapper at send time"}
		}
		wire := connector.Preview(t, enc.Input)
		var rendered map[string]any
		if b, ok := wire["bridge"].(map[string]any); ok {
			rendered, _ = b["action"].(map[string]any)
		} else {
			rendered, err = connector.Render(t, prev)
			if err != nil {
				writeErr(w, 400, fmt.Errorf("request_template: %w", err))
				return
			}
		}
		out["request"] = rendered
		out["wire"] = wire
		if t.Mapper.Enabled {
			out["mapper"] = map[string]any{"model": t.Mapper.Model, "note": "The request fields marked {{mapped}} are written by an LLM when you send, and are never evidence."}
		}
		if t.DirectEndpoint != "" {
			direct, err := connector.RenderDirect(t, connector.Input{Source: "chat/text", Content: enc.Input.Content, Canary: canary})
			if err != nil {
				writeErr(w, 400, fmt.Errorf("direct_template: %w", err))
				return
			}
			out["direct_request"] = direct
		}
	}
	writeJSON(w, 200, out)
}

// runError maps an engine validation failure to a status code.
func runError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, err)
		return
	}
	writeErr(w, 400, err)
}

func (s *Server) createRun(w http.ResponseWriter, r *http.Request) {
	var req engine.Request
	if !decode(w, r, &req) {
		return
	}
	req.Mode, req.RunID, req.CampaignID = "repeater", "", ""
	if _, err := s.engine(nil).Validate(req); err != nil {
		runError(w, err)
		return
	}
	if r.URL.Query().Get("async") == "1" {
		id := engine.NewID()
		req.RunID = id
		j := s.jobs.start(id)
		ctx, end := s.active.begin(s.base)
		go func() {
			defer s.jobs.forget(id)
			defer end()
			eng := s.engine(func(_ string, t model.Trial) { j.emit("trial", t, false) })
			run, err := eng.Run(ctx, req)
			if err != nil {
				run.Error = err.Error()
			}
			j.emit("done", run, true)
		}()
		writeJSON(w, http.StatusAccepted, map[string]string{"run_id": id})
		return
	}
	ctx, end := s.active.begin(r.Context())
	defer end()
	run, err := s.engine(nil).Run(ctx, req)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, 200, run)
}

func (s *Server) listRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := s.st.ListRuns(limitParam(r, 50, 500))
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	if runs == nil {
		runs = []model.Run{}
	}
	if r.URL.Query().Get("full") != "1" {
		// Trials carry raw responses and rendered requests; the list needs
		// only the verdict.
		for i := range runs {
			runs[i].Trials = nil
		}
	}
	writeJSON(w, 200, runs)
}

func (s *Server) getRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.st.GetRun(r.PathValue("id"))
	if err != nil {
		storeErr(w, err)
		return
	}
	writeJSON(w, 200, run)
}

func (s *Server) runEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if j := s.jobs.get(id); j != nil {
		j.stream(w, r)
		return
	}
	run, err := s.st.GetRun(id)
	if err != nil {
		storeErr(w, err)
		return
	}
	writeOneShot(w, "done", run)
}

func artifactKey(runID, arm, index string) string { return runID + "/" + arm + "/" + index }

func artifactType(kind string) (contentType, ext string) {
	switch kind {
	case "image/png":
		return "image/png", "png"
	case "application/pdf":
		return "application/pdf", "pdf"
	case "text/calendar":
		return "text/calendar; charset=utf-8", "ics"
	case "message/rfc822":
		return "message/rfc822", "eml"
	case "application/json":
		return "application/json", "json"
	case "application/x-ndef":
		return "application/octet-stream", "ndef"
	case "application/x-ble-ad":
		return "application/octet-stream", "bleadv"
	}
	return "audio/wav", "wav" // pulses are stored as a Morse sidetone WAV
}

func (s *Server) getArtifact(w http.ResponseWriter, r *http.Request) {
	run, err := s.st.GetRun(r.PathValue("id"))
	if err != nil {
		storeErr(w, err)
		return
	}
	if run.Channel.Artifact == nil {
		writeErr(w, 404, errors.New("run has no artifact"))
		return
	}
	data, err := s.st.GetArtifact(artifactKey(run.ID, r.PathValue("arm"), r.PathValue("index")))
	if err != nil {
		storeErr(w, err)
		return
	}
	ct, _ := artifactType(run.Channel.Artifact.Kind)
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}

// getReplyArtifact serves the far end's audio from a SIP trial.
func (s *Server) getReplyArtifact(w http.ResponseWriter, r *http.Request) {
	data, err := s.st.GetArtifact(artifactKey(r.PathValue("id"), r.PathValue("arm"), r.PathValue("index")) + "/reply")
	if err != nil {
		storeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "audio/wav")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}

// getObserverShot serves a browser observer screenshot of one trial.
func (s *Server) getObserverShot(w http.ResponseWriter, r *http.Request) {
	phase := r.PathValue("phase")
	if phase != "before" && phase != "after" {
		writeErr(w, 404, errors.New("not found"))
		return
	}
	data, err := s.st.GetArtifact(artifactKey(r.PathValue("id"), r.PathValue("arm"), r.PathValue("index")) + "/observer-" + phase)
	if err != nil {
		storeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}

// getBundle streams the evidence for one run: run.json, every stored
// artifact, the sink events seen by its trials, and a manifest of SHA-256
// sums so the bundle can back a knowledge-base claim.
func (s *Server) getBundle(w http.ResponseWriter, r *http.Request) {
	run, err := s.st.GetRun(r.PathValue("id"))
	if err != nil {
		storeErr(w, err)
		return
	}
	files := map[string][]byte{}
	files["run.json"], _ = json.MarshalIndent(run, "", "  ")

	var events []model.SinkEvent
	seen := map[int64]bool{}
	for _, t := range run.Trials {
		for _, ev := range t.Sink {
			if !seen[ev.ID] {
				seen[ev.ID] = true
				events = append(events, ev)
			}
		}
		if t.Observer != nil {
			for _, o := range []*model.Observation{t.Observer.Before, t.Observer.After} {
				if o == nil || o.ScreenshotRef == "" {
					continue
				}
				if shot, err := s.st.GetArtifact(o.ScreenshotRef); err == nil {
					files[fmt.Sprintf("artifacts/%s-%d-observer-%s.png", t.Arm, t.Index, o.Phase)] = shot
				}
			}
		}
		if run.Channel.Artifact == nil || t.Arm == "direct" {
			continue
		}
		idx := strconv.Itoa(t.Index)
		data, err := s.st.GetArtifact(artifactKey(run.ID, t.Arm, idx))
		if err != nil {
			continue
		}
		_, ext := artifactType(run.Channel.Artifact.Kind)
		files["artifacts/"+t.Arm+"-"+idx+"."+ext] = data
		if t.Exchange.ReplyArtifact != "" {
			if reply, err := s.st.GetArtifact(t.Exchange.ReplyArtifact); err == nil {
				files["artifacts/"+t.Arm+"-"+idx+"-reply.wav"] = reply
			}
		}
	}
	if events == nil {
		events = []model.SinkEvent{}
	}
	files["sink_events.json"], _ = json.MarshalIndent(events, "", "  ")
	// Per arm: hits are executed forbidden calls, attempted adds the ones the
	// target refused. target_info records the layers and temperature.
	files["summary.json"], _ = json.MarshalIndent(map[string]any{
		"outcome": run.Verdict.Outcome, "arms": run.Verdict.Arms, "target_info": run.TargetInfo, "variants": run.Variants,
	}, "", "  ")

	names := make([]string, 0, len(files))
	manifest := map[string]string{}
	for n, b := range files {
		names = append(names, n)
		sum := sha256.Sum256(b)
		manifest[n] = hex.EncodeToString(sum[:])
	}
	sort.Strings(names)
	manifestJSON, _ := json.MarshalIndent(manifest, "", "  ")

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	write := func(name string, b []byte) error {
		f, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: run.StartedAt})
		if err != nil {
			return err
		}
		_, err = f.Write(b)
		return err
	}
	for _, n := range names {
		if err := write(n, files[n]); err != nil {
			writeErr(w, 500, err)
			return
		}
	}
	if err := write("manifest.sha256.json", manifestJSON); err != nil {
		writeErr(w, 500, err)
		return
	}
	if err := zw.Close(); err != nil {
		writeErr(w, 500, err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="crosswire-run-%s.zip"`, run.ID))
	_, _ = w.Write(buf.Bytes())
}

type intruderRequest struct {
	TargetID       string         `json:"target_id"`
	Adapter        string         `json:"adapter"`
	FieldsTemplate map[string]any `json:"fields_template"`
	Payloads       []string       `json:"payloads"`
	ControlFields  map[string]any `json:"control_fields"`
	Direct         bool           `json:"direct"`
	Repeats        int            `json:"repeats"`
	Concurrency    int            `json:"concurrency"`
	ProductionAck  bool           `json:"production_acknowledged"`
}

type intruderRow struct {
	Payload string        `json:"payload"`
	RunID   string        `json:"run_id,omitempty"`
	Verdict model.Verdict `json:"verdict"`
	Error   string        `json:"error,omitempty"`
}

type intruderResult struct {
	CampaignID string        `json:"campaign_id"`
	Runs       []intruderRow `json:"runs"`
}

const payloadMarker = "{{payload}}"

// fieldsFor substitutes one payload into the template. Only string values
// are touched.
func fieldsFor(tpl map[string]any, payload string) map[string]any {
	out := make(map[string]any, len(tpl))
	for k, v := range tpl {
		if s, ok := v.(string); ok {
			out[k] = strings.ReplaceAll(s, payloadMarker, payload)
			continue
		}
		out[k] = v
	}
	return out
}

func (ir intruderRequest) check() error {
	if len(ir.Payloads) == 0 {
		return errors.New("payloads is empty")
	}
	if len(ir.Payloads) > maxPayloads {
		return fmt.Errorf("at most %d payloads per campaign", maxPayloads)
	}
	for i, p := range ir.Payloads {
		if strings.TrimSpace(p) == "" {
			return fmt.Errorf("payload %d is blank", i+1)
		}
	}
	markers := 0
	for _, v := range ir.FieldsTemplate {
		if s, ok := v.(string); ok {
			markers += strings.Count(s, payloadMarker)
		}
	}
	if markers != 1 {
		return fmt.Errorf("fields_template must contain exactly one %s marker, found %d", payloadMarker, markers)
	}
	return nil
}

func (ir intruderRequest) engineRequest(campaign string, payload string) engine.Request {
	return engine.Request{
		TargetID: ir.TargetID, Mode: "intruder", Adapter: ir.Adapter,
		Fields: fieldsFor(ir.FieldsTemplate, payload), ControlFields: ir.ControlFields,
		Direct: ir.Direct, Repeats: ir.Repeats, Concurrency: ir.Concurrency,
		ProductionAck: ir.ProductionAck, CampaignID: campaign,
	}
}

// runCampaign runs the payloads one after another: sink events are matched
// to trials by time window, so overlapping runs would blur attribution.
func (s *Server) runCampaign(ctx context.Context, ir intruderRequest, campaign string, onRow func(intruderRow)) intruderResult {
	res := intruderResult{CampaignID: campaign, Runs: []intruderRow{}}
	eng := s.engine(nil)
	for _, p := range ir.Payloads {
		row := intruderRow{Payload: p}
		if ctx.Err() != nil {
			row.Error = "cancelled"
		} else {
			run, err := eng.Run(ctx, ir.engineRequest(campaign, p))
			row.Verdict = run.Verdict
			if err != nil {
				row.Error = err.Error()
			} else {
				row.RunID = run.ID
			}
		}
		res.Runs = append(res.Runs, row)
		if onRow != nil {
			onRow(row)
		}
	}
	return res
}

func (s *Server) createIntruder(w http.ResponseWriter, r *http.Request) {
	var ir intruderRequest
	if !decode(w, r, &ir) {
		return
	}
	if err := ir.check(); err != nil {
		writeErr(w, 400, err)
		return
	}
	campaign := "cmp-" + engine.NewID()
	if _, err := s.engine(nil).Validate(ir.engineRequest(campaign, ir.Payloads[0])); err != nil {
		runError(w, err)
		return
	}
	if r.URL.Query().Get("async") == "1" {
		j := s.jobs.start(campaign)
		ctx, end := s.active.begin(s.base)
		go func() {
			defer s.jobs.forget(campaign)
			defer end()
			res := s.runCampaign(ctx, ir, campaign, func(row intruderRow) { j.emit("run", row, false) })
			j.emit("done", res, true)
		}()
		writeJSON(w, http.StatusAccepted, map[string]string{"campaign_id": campaign})
		return
	}
	ctx, end := s.active.begin(r.Context())
	defer end()
	writeJSON(w, 200, s.runCampaign(ctx, ir, campaign, nil))
}

func (s *Server) intruderEvents(w http.ResponseWriter, r *http.Request) {
	j := s.jobs.get(r.PathValue("id"))
	if j == nil {
		writeErr(w, 404, errors.New("campaign not found or already expired"))
		return
	}
	j.stream(w, r)
}
