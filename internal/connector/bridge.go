package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/rayhanhanaputra/crosswire/internal/guard"
	"github.com/rayhanhanaputra/crosswire/internal/model"
)

// ErrStopNotConfirmed marks an exchange whose emergency stop did not come
// back confirmed. The engine halts the run: a stimulus may still be active.
var ErrStopNotConfirmed = errors.New("emergency stop was not confirmed")

var hardwareID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:._/-]{0,127}$`)

const (
	bridgeMaxDurationMs = 30000
	bridgeMaxUnits      = 256
)

// BridgeOperation is the operation name a bridge kind uses on the wire.
func BridgeOperation(kind string) string { return model.BridgeOperation(kind) }

// BridgeAdapters lists the channels a bridge kind can carry.
func BridgeAdapters(kind string) []string {
	switch kind {
	case "ble":
		return []string{"ble-advert"}
	case "nfc":
		return []string{"nfc-tag"}
	case "gpio", "led":
		return []string{"binary-toggle", "morse-led"}
	}
	return nil
}

// DefaultBridgeTemplate is the action body for a kind when the target sets none.
func DefaultBridgeTemplate(kind string) string {
	head := `{"device_id":"{{device_id}}","operation":"{{operation}}","resource":"{{resource}}","duration_ms":{{duration_ms}}`
	switch kind {
	case "ble":
		return head + `,"input":{"advertisement_hex":"{{artifact_hex}}","name":"{{content}}"}}`
	case "nfc":
		return head + `,"input":{"ndef_hex":"{{artifact_hex}}","text":"{{content}}"}}`
	}
	return head + `,"unit_ms":{{unit_ms}},"input":{"blink_units":{{blink_units}},"content":"{{content}}"}}`
}

// DefaultBridgeStop is the stop body when the target sets none.
const DefaultBridgeStop = `{"device_id":"{{device_id}}","operation":"{{operation}}","resource":"{{resource}}","reason":"assessment_complete"}`

// CheckBridge validates a bridge configuration against the target's endpoint.
// Nothing physical may be sent until every one of these holds.
func CheckBridge(endpoint string, cfg model.BridgeConfig) error {
	if BridgeOperation(cfg.Kind) == "" {
		return errors.New("bridge kind must be ble, nfc, gpio or led")
	}
	action, err := guard.ParseURL(endpoint, "http", "https")
	if err != nil {
		return fmt.Errorf("bridge action endpoint: %w", err)
	}
	stop, err := guard.ParseURL(cfg.StopEndpoint, "http", "https")
	if err != nil {
		return fmt.Errorf("bridge stop endpoint: %w", err)
	}
	if action.Scheme != stop.Scheme || action.Host != stop.Host {
		return errors.New("bridge action and stop endpoints must share an origin")
	}
	if action.Scheme == "http" && !guard.IsLoopbackHost(action.Hostname()) {
		return errors.New("a bridge that is not on this machine must use HTTPS")
	}
	if !hardwareID.MatchString(cfg.DeviceID) || !hardwareID.MatchString(cfg.Resource) {
		return errors.New("bridge device_id and resource are required and may use letters, digits and : . _ / - only")
	}
	if !contains(cfg.AllowedDeviceIDs, cfg.DeviceID) {
		return errors.New("the selected device must be on the device allowlist")
	}
	if !contains(cfg.AllowedResources, cfg.Resource) {
		return errors.New("the selected resource must be on the resource allowlist")
	}
	for _, id := range append(append([]string(nil), cfg.AllowedDeviceIDs...), cfg.AllowedResources...) {
		if !hardwareID.MatchString(id) {
			return fmt.Errorf("allowlist entry %q is not a valid identifier", id)
		}
	}
	if len(cfg.AllowedDeviceIDs) > 20 || len(cfg.AllowedResources) > 50 {
		return errors.New("allowlists are limited to 20 devices and 50 resources")
	}
	if cfg.DurationMs != 0 && (cfg.DurationMs < 100 || cfg.DurationMs > bridgeMaxDurationMs) {
		return errors.New("bridge duration_ms must be between 100 and 30000")
	}
	if cfg.UnitMs != 0 && (cfg.UnitMs < 50 || cfg.UnitMs > 2000) {
		return errors.New("bridge unit_ms must be between 50 and 2000")
	}
	return nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// blinkDuration returns the length of a blink_units array in unit counts.
func blinkUnits(raw string) (steps, units int, err error) {
	var list [][2]any
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return 0, 0, errors.New("the channel produced no blink_units")
	}
	for _, s := range list {
		n, ok := s[1].(float64)
		if !ok || n < 1 || n > 20 {
			return 0, 0, errors.New("a pulse step must be 1 to 20 units")
		}
		units += int(n)
	}
	return len(list), units, nil
}

// RenderBridge fills the action and stop bodies of a bridge target.
func RenderBridge(t model.Target, in Input) (action, stop map[string]any, err error) {
	cfg := t.Bridge
	duration := cfg.DurationMs
	if duration == 0 {
		duration = 5000
	}
	in.Extra = map[string]string{"device_id": cfg.DeviceID, "operation": model.BridgeOperation(cfg.Kind), "resource": cfg.Resource, "duration_ms": fmt.Sprint(duration)}
	if in.UnitMs == 0 {
		in.UnitMs = cfg.UnitMs
	}
	if action, err = render(t.RequestTemplate, DefaultBridgeTemplate(cfg.Kind), in); err != nil {
		return nil, nil, err
	}
	if stop, err = render(cfg.StopTemplate, DefaultBridgeStop, in); err != nil {
		return nil, nil, fmt.Errorf("stop template: %w", err)
	}
	return action, stop, nil
}

type httpResult struct {
	status int
	raw    string
}

// postJSON sends one JSON request without following redirects.
func postJSON(ctx context.Context, method, endpoint string, headers map[string]string, body any, timeout time.Duration) (httpResult, error) {
	var res httpResult
	enc, _ := json.Marshal(body)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(enc))
	if err != nil {
		return res, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirects are not followed") },
		Transport:     &http.Transport{DialContext: guard.DialContext(true, 10*time.Second), TLSHandshakeTimeout: 10 * time.Second},
	}
	resp, err := client.Do(req)
	if err != nil {
		return res, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if len(raw) > maxResponseBytes {
		return res, errors.New("bridge response exceeds 2 MiB")
	}
	return httpResult{status: resp.StatusCode, raw: string(raw)}, nil
}

// sendBridge drives a physical stimulus through an HTTP device bridge. The
// order is fixed: allowlist and limit checks, the action request, the
// AfterAction hook (observer reads while the stimulus is active), then the
// stop request, which is sent every time an action request was attempted. The
// stop must come back 2xx or the exchange is an error (ErrStopNotConfirmed).
func sendBridge(ctx context.Context, t model.Target, in Input) (model.Exchange, error) {
	var ex model.Exchange
	start := time.Now()
	cfg := t.Bridge
	if err := CheckBridge(t.Endpoint, cfg); err != nil {
		return fail(ex, start, err)
	}
	op := BridgeOperation(cfg.Kind)
	duration := cfg.DurationMs
	if duration == 0 {
		duration = 5000
	}
	if cfg.Kind == "led" || cfg.Kind == "gpio" {
		unit := in.UnitMs
		if unit == 0 {
			unit = cfg.UnitMs
		}
		if unit == 0 {
			unit = 200
		}
		if unit < 50 || unit > 2000 {
			return fail(ex, start, fmt.Errorf("pulse unit %d ms is outside the bridge's 50 to 2000 ms range", unit))
		}
		steps, units, err := blinkUnits(in.BlinkUnits)
		if err != nil {
			return fail(ex, start, fmt.Errorf("%s bridge: %w (use the binary-toggle or morse-led channel)", cfg.Kind, err))
		}
		if steps == 0 || steps > bridgeMaxUnits {
			return fail(ex, start, fmt.Errorf("a sequence needs 1 to %d steps, this one has %d", bridgeMaxUnits, steps))
		}
		if total := units * unit; total > duration {
			return fail(ex, start, fmt.Errorf("the sequence runs %d ms but this target allows %d ms (duration_ms)", total, duration))
		}
		in.UnitMs = unit
	} else if in.ArtifactHex == "" {
		return fail(ex, start, fmt.Errorf("%s bridge needs the %s channel", cfg.Kind, BridgeAdapters(cfg.Kind)[0]))
	}
	action, stopBody, err := RenderBridge(t, in)
	if err != nil {
		return fail(ex, start, err)
	}
	headers, err := StaticAndSecretHeaders(t)
	if err != nil {
		return fail(ex, start, err)
	}
	ex.Request = action
	rec := &model.BridgeRecord{Kind: cfg.Kind, DeviceID: cfg.DeviceID, Resource: cfg.Resource, Operation: op, DurationMs: duration}
	ex.Bridge = rec

	res, actionErr := postJSON(ctx, http.MethodPost, t.Endpoint, headers, action, Timeout(t))
	rec.ActionAtMs = int(time.Since(start).Milliseconds())
	rec.AckStatus, rec.Ack = res.status, res.raw
	if actionErr == nil && (res.status < 200 || res.status > 299) {
		actionErr = fmt.Errorf("bridge refused the action: HTTP %d", res.status)
	}
	var holdErr error
	if actionErr == nil && in.AfterAction != nil {
		holdErr = in.AfterAction(ctx)
	}

	// The stop is not tied to the caller's context: a cancelled run must still
	// switch the stimulus off.
	stopCtx, stopCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer stopCancel()
	stopRes, stopErr := postJSON(stopCtx, http.MethodPost, cfg.StopEndpoint, headers, stopBody, 10*time.Second)
	rec.StopAtMs = int(time.Since(start).Milliseconds())
	rec.StopStatus = stopRes.status
	if stopErr == nil && (stopRes.status < 200 || stopRes.status > 299) {
		stopErr = fmt.Errorf("bridge returned HTTP %d for the stop", stopRes.status)
	}
	ex.DurationMs = rec.StopAtMs
	ex.Status = res.status
	if stopErr != nil {
		rec.StopError = stopErr.Error()
		err := fmt.Errorf("%w: %v", ErrStopNotConfirmed, stopErr)
		if actionErr != nil {
			err = fmt.Errorf("%w; the action also failed: %v", err, actionErr)
		}
		ex.Error = err.Error()
		return ex, err
	}
	rec.StopConfirmed = true
	if actionErr != nil {
		ex.Error = "bridge: " + actionErr.Error()
		return ex, errors.New(ex.Error)
	}
	if holdErr != nil {
		ex.Error = "observer: " + holdErr.Error()
		return ex, errors.New(ex.Error)
	}
	Finish(t, &ex, strings.ToValidUTF8(res.raw, "�"))
	return ex, nil
}
