package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/channel"
	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/connector"
	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/evidence"
	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/mapper"
	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/model"
	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/observer"
)

// timeline collects a trial's steps in time order.
type timeline struct {
	start time.Time
	ev    []model.TimelineEvent
}

func (t *timeline) add(source, event, detail string) { t.addAt(time.Now(), source, event, detail) }

func (t *timeline) addAt(at time.Time, source, event, detail string) {
	ms := int(at.Sub(t.start).Milliseconds())
	if ms < 0 {
		ms = 0
	}
	t.ev = append(t.ev, model.TimelineEvent{AtMs: ms, Source: source, Event: event, Detail: detail})
}

func (t *timeline) sorted() []model.TimelineEvent {
	sort.SliceStable(t.ev, func(i, j int) bool { return t.ev[i].AtMs < t.ev[j].AtMs })
	return t.ev
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func describeArtifact(adapter string, enc channel.Encoded) string {
	d := adapter
	if enc.Modeled {
		d += " (modeled)"
	}
	if enc.Artifact != nil {
		d += ": " + trunc(enc.Artifact.Summary, 120)
	}
	return d
}

// describeSend names the transport and destination without any query string or
// credentials.
func describeSend(t model.Target, arm connector.Arm) string {
	endpoint := t.Endpoint
	switch arm {
	case connector.Direct:
		endpoint = t.DirectEndpoint
	case connector.Trigger:
		endpoint = t.TriggerEndpoint
	}
	transport := connector.PathTransport(t, arm, endpoint)
	if u, err := url.Parse(endpoint); err == nil && u.Host != "" {
		endpoint = u.Scheme + "://" + u.Host + u.Path
	}
	return transport + " " + endpoint
}

func describeReply(ex model.Exchange) string {
	switch {
	case ex.Error != "":
		return "error: " + trunc(ex.Error, 160)
	case ex.Status != 0:
		return fmt.Sprintf("HTTP %d in %d ms", ex.Status, ex.DurationMs)
	}
	return fmt.Sprintf("reply in %d ms", ex.DurationMs)
}

func mapperDetail(m *model.MappedInput) string {
	if m == nil {
		return ""
	}
	if m.Error != "" {
		return "failed: " + m.Error
	}
	return fmt.Sprintf("by %s (mapped, not evidence); canary kept: %v", m.Model, m.CarriesCanary)
}

// recordBridge adds the bridge's acknowledgement and stop confirmation to the
// trial timeline. Offsets in the record count from the start of the exchange.
func (e *Engine) recordBridge(tl *timeline, began, sent time.Time, ex model.Exchange) {
	b := ex.Bridge
	if b == nil {
		return
	}
	tl.addAt(sent.Add(time.Duration(b.ActionAtMs)*time.Millisecond), "bridge",
		"action acknowledged", fmt.Sprintf("%s on %s/%s, HTTP %d", b.Operation, b.DeviceID, b.Resource, b.AckStatus))
	if b.StopConfirmed {
		tl.addAt(sent.Add(time.Duration(b.StopAtMs)*time.Millisecond), "bridge", "emergency stop confirmed", fmt.Sprintf("HTTP %d", b.StopStatus))
	} else {
		tl.addAt(sent.Add(time.Duration(b.StopAtMs)*time.Millisecond), "bridge", "emergency stop NOT confirmed", b.StopError)
	}
}

// mapsPath reports whether the input mapper applies to this arm: the mapper is
// on and that path's request template asks for {{mapped}}.
func (e *Engine) mapsPath(t model.Target, arm connector.Arm) bool {
	if !t.Mapper.Enabled {
		return false
	}
	tpl := t.RequestTemplate
	switch arm {
	case connector.Direct:
		tpl = t.DirectTemplate
	case connector.Trigger:
		tpl = t.TriggerTemplate
	}
	return strings.Contains(tpl, "{{mapped}}")
}

// mapInput runs the input mapper and stores its output on the input.
func (e *Engine) mapInput(ctx context.Context, t model.Target, in *connector.Input) *model.MappedInput {
	rec := &model.MappedInput{Model: t.Mapper.Model}
	msg := mapper.Message{Source: in.Source, Content: in.Content, Attachments: map[string]map[string]any{}}
	for kind, b64 := range map[string]string{"image": in.ImageBase64, "audio": in.AudioBase64, "file": in.FileBase64, "artifact": in.ArtifactBase64} {
		if b64 != "" {
			msg.Attachments[kind] = map[string]any{"base64_characters": len(b64)}
		}
	}
	out, err := mapper.MapInput(ctx, t.Mapper, msg)
	if err != nil {
		rec.Error = err.Error()
		return rec
	}
	rec.Output = out
	enc, _ := json.Marshal(out)
	rec.CarriesCanary = evidence.Carries(string(enc), in.Canary)
	in.Mapped = out
	return rec
}

// mapResponse runs the response mapper on a captured exchange. A failure is
// recorded and the deterministic extraction stays in force.
func (e *Engine) mapResponse(ctx context.Context, t model.Target, ex *model.Exchange, tl *timeline) {
	if !t.ResponseMapper.Enabled || ex.Error != "" || ex.RawResponse == "" {
		return
	}
	res, err := mapper.MapResponse(ctx, t.ResponseMapper, mapper.Reply{Raw: ex.RawResponse, Text: ex.ResponseText, Actions: ex.Actions,
		Metadata: map[string]any{"status": ex.Status}})
	if err != nil {
		res.Error = err.Error()
	}
	ex.MappedResponse = &res
	if err != nil {
		tl.add("response mapper", "failed", res.Error)
		return
	}
	tl.add("response mapper", "reply mapped", fmt.Sprintf("by %s (mapped, not evidence): %d action(s)", res.Model, len(res.Actions)))
}

// validateTransport checks the parts of a request that depend on how the target
// is reached: what the channel must produce, what needs acknowledgement, and
// what must be installed.
func (e *Engine) validateTransport(t model.Target, req Request) error {
	enc, _ := e.Encoder.Encode(context.Background(), req.Adapter, channel.Substitute(req.Fields, "cw-preview"))
	switch t.Transport {
	case "sip":
		if enc.Artifact == nil || enc.Artifact.Kind != "audio/wav" {
			return errors.New("a sip target takes audio only: use the speech or dtmf adapter")
		}
	case "voice-ws":
		if enc.Artifact == nil || enc.Artifact.Kind != "audio/wav" {
			return errors.New("a voice-ws target takes audio only: use the speech, dtmf or audio-upload adapter")
		}
	case "bridge":
		if !req.ProductionAck {
			return errors.New("device bridge targets drive physical hardware and require explicit acknowledgement")
		}
		if err := connector.CheckBridge(t.Endpoint, t.Bridge); err != nil {
			return fmt.Errorf("bridge: %w", err)
		}
		ok := false
		for _, a := range connector.BridgeAdapters(t.Bridge.Kind) {
			ok = ok || a == req.Adapter
		}
		if !ok {
			return fmt.Errorf("a %s bridge carries the %s channel, not %q", t.Bridge.Kind, strings.Join(connector.BridgeAdapters(t.Bridge.Kind), " or "), req.Adapter)
		}
		if len(t.Bridge.AllowedDeviceIDs) == 0 {
			return errors.New("bridge: the device allowlist is empty")
		}
	}
	if req.Adapter == "audio-upload" && (t.Evidence.RequireCanary == nil || *t.Evidence.RequireCanary) {
		return errors.New("audio-upload sends a fixed recording, which cannot carry the per-trial canary, but this target requires the canary in tool arguments; turn off 'require canary' on the target to judge by session instead")
	}
	if err := connector.CheckProduction(t); err != nil {
		return err
	}
	if o := observer.New(t.Observer); o != nil {
		if err := o.Preflight(); err != nil {
			return err
		}
	}
	if t.Mapper.Enabled {
		if err := mapper.CheckEndpoint(t.Mapper.Endpoint); err != nil {
			return fmt.Errorf("input mapper: %w", err)
		}
	}
	if t.ResponseMapper.Enabled {
		if err := mapper.CheckEndpoint(t.ResponseMapper.Endpoint); err != nil {
			return fmt.Errorf("response mapper: %w", err)
		}
	}
	for _, d := range []struct{ label, env string }{{"input mapper", t.Mapper.APIKeyEnv}, {"response mapper", t.ResponseMapper.APIKeyEnv}} {
		enabled := (d.label == "input mapper" && t.Mapper.Enabled) || (d.label == "response mapper" && t.ResponseMapper.Enabled)
		if enabled && !envSet(d.env) {
			return fmt.Errorf("%s: environment variable %s is not set", d.label, d.env)
		}
	}
	return nil
}

func envSet(name string) bool { return name != "" && os.Getenv(name) != "" }
