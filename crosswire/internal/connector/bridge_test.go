package connector

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/fakes"
	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/model"
)

func bridgeTarget(b *fakes.Bridge, kind string) model.Target {
	return model.Target{
		Transport: "bridge", Endpoint: b.ActionURL(), TimeoutSeconds: 5,
		Bridge: model.BridgeConfig{Kind: kind, StopEndpoint: b.StopURL(), DeviceID: "arduino-01", Resource: "pin-9",
			AllowedDeviceIDs: []string{"arduino-01"}, AllowedResources: []string{"pin-9"}, DurationMs: 5000},
	}
}

func ledInput() Input {
	return Input{Source: "light-sensor/morse", Content: "...", Canary: canary, BlinkUnits: `[["on",1],["off",1],["on",3]]`, UnitMs: 100}
}

func TestBridgeLEDRecordsAckAndStop(t *testing.T) {
	b := fakes.NewBridge("led", "arduino-01", "pin-9")
	defer b.Close()
	in := ledInput()
	var activeDuringHold bool
	in.AfterAction = func(ctx context.Context) error { activeDuringHold = b.Sim.Active(); return nil }
	ex, err := send(t, bridgeTarget(b, "led"), Perception, in)
	if err != nil {
		t.Fatal(err)
	}
	if !activeDuringHold {
		t.Error("the stimulus must still be active while the hook runs")
	}
	if b.Sim.Active() || strings.Join(b.Sim.Log(), ",") != "start,stop" {
		t.Errorf("order: %v active=%v", b.Sim.Log(), b.Sim.Active())
	}
	r := ex.Bridge
	if r == nil || r.Kind != "led" || r.DeviceID != "arduino-01" || r.Resource != "pin-9" || r.Operation != "led_sequence" ||
		r.AckStatus != 200 || !strings.Contains(r.Ack, `"accepted":true`) || r.StopStatus != 200 || !r.StopConfirmed || r.StopAtMs < r.ActionAtMs {
		t.Fatalf("bridge record: %+v", r)
	}
	if got := b.Sim.Last(); len(got.Pulses) != 3 || got.UnitMs != 100 {
		t.Errorf("the bridge saw %+v", got)
	}
	if ex.Request["resource"] != "pin-9" || ex.Request["operation"] != "led_sequence" {
		t.Errorf("request: %v", ex.Request)
	}
}

func TestBridgeBLEAndNFCUseTheArtifact(t *testing.T) {
	for kind, op := range map[string]string{"ble": "ble_advertise", "nfc": "nfc_emulate"} {
		b := fakes.NewBridge(kind, "arduino-01", "pin-9")
		in := Input{Content: "Lock cw-1", Canary: canary, ArtifactHex: "020106"}
		ex, err := send(t, bridgeTarget(b, kind), Perception, in)
		b.Close()
		if err != nil || ex.Bridge.Operation != op {
			t.Errorf("%s: %v %+v", kind, err, ex.Bridge)
		}
		if _, err := send(t, bridgeTarget(fakes.NewBridge(kind, "arduino-01", "pin-9"), kind), Perception, Input{Content: "x"}); err == nil || !strings.Contains(err.Error(), "needs the") {
			t.Errorf("%s without an artifact: %v", kind, err)
		}
	}
}

func TestBridgeRefusesBeforeSendingAnything(t *testing.T) {
	b := fakes.NewBridge("led", "arduino-01", "pin-9")
	defer b.Close()
	cases := map[string]func(*model.Target, *Input){
		"device not allowlisted":   func(t *model.Target, _ *Input) { t.Bridge.AllowedDeviceIDs = []string{"other"} },
		"resource not allowlisted": func(t *model.Target, _ *Input) { t.Bridge.AllowedResources = []string{"pin-3"} },
		"no stop endpoint":         func(t *model.Target, _ *Input) { t.Bridge.StopEndpoint = "" },
		"different origin":         func(t *model.Target, _ *Input) { t.Bridge.StopEndpoint = "http://127.0.0.1:1/stop" },
		"duration too long":        func(t *model.Target, _ *Input) { t.Bridge.DurationMs = 100 },
		"duration over the cap":    func(t *model.Target, _ *Input) { t.Bridge.DurationMs = 60000 },
		"unit out of range":        func(_ *model.Target, in *Input) { in.UnitMs = 10 },
		"no blink units":           func(_ *model.Target, in *Input) { in.BlinkUnits = "" },
		"step too long":            func(_ *model.Target, in *Input) { in.BlinkUnits = `[["on",21]]` },
		"bad kind":                 func(t *model.Target, _ *Input) { t.Bridge.Kind = "radio" },
		"bad template":             func(t *model.Target, _ *Input) { t.RequestTemplate = `{"x":` },
		"http off-host": func(t *model.Target, _ *Input) {
			t.Endpoint, t.Bridge.StopEndpoint = "http://10.1.2.3/a", "http://10.1.2.3/s"
		},
	}
	for name, mut := range cases {
		tg, in := bridgeTarget(b, "led"), ledInput()
		mut(&tg, &in)
		if _, err := send(t, tg, Perception, in); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if len(b.Sim.Log()) != 0 {
		t.Errorf("a refused exchange must not touch the bridge, got %v", b.Sim.Log())
	}
}

func TestBridgeStopFailureIsReportedAndSentinel(t *testing.T) {
	b := fakes.NewBridge("led", "arduino-01", "pin-9")
	defer b.Close()
	b.Sim.FailStop = true
	ex, err := send(t, bridgeTarget(b, "led"), Perception, ledInput())
	if !errors.Is(err, ErrStopNotConfirmed) || ex.Error == "" || ex.Bridge == nil || ex.Bridge.StopConfirmed || ex.Bridge.StopError == "" {
		t.Fatalf("%v %+v", err, ex.Bridge)
	}
}

func TestBridgeActionRefusedStillSendsStop(t *testing.T) {
	b := fakes.NewBridge("led", "arduino-01", "pin-9")
	defer b.Close()
	tg := bridgeTarget(b, "led")
	// Make the bridge reject the action: its device id differs from the target's.
	tg.Bridge.DeviceID, tg.Bridge.AllowedDeviceIDs = "other-device", []string{"other-device"}
	ex, err := send(t, tg, Perception, ledInput())
	if err == nil || ex.Bridge == nil || ex.Bridge.AckStatus != 400 {
		t.Fatalf("%v %+v", err, ex.Bridge)
	}
	if errors.Is(err, ErrStopNotConfirmed) {
		t.Log("the fake also rejects the stop for the wrong device, which is correct")
	}
}

func TestBridgeStopIsSentWhenTheRunIsCancelled(t *testing.T) {
	b := fakes.NewBridge("led", "arduino-01", "pin-9")
	defer b.Close()
	ctx, cancel := context.WithCancel(context.Background())
	in := ledInput()
	in.AfterAction = func(ctx context.Context) error { cancel(); <-ctx.Done(); return ctx.Err() }
	ex, err := Send(ctx, bridgeTarget(b, "led"), Perception, in)
	if err == nil {
		t.Error("a cancelled hold is an error")
	}
	if strings.Join(b.Sim.Log(), ",") != "start,stop" || ex.Bridge == nil || !ex.Bridge.StopConfirmed {
		t.Errorf("a cancelled run must still stop the stimulus: %v %+v", b.Sim.Log(), ex.Bridge)
	}
}

func TestCheckBridgeAllowlistRules(t *testing.T) {
	ok := model.BridgeConfig{Kind: "ble", StopEndpoint: "http://127.0.0.1:9/stop", DeviceID: "hci0", Resource: "adv0",
		AllowedDeviceIDs: []string{"hci0"}, AllowedResources: []string{"adv0"}}
	if err := CheckBridge("http://127.0.0.1:9/action", ok); err != nil {
		t.Fatal(err)
	}
	bad := ok
	bad.DeviceID = "has space"
	bad.AllowedDeviceIDs = []string{"has space"}
	if CheckBridge("http://127.0.0.1:9/action", bad) == nil {
		t.Error("invalid identifier")
	}
	bad = ok
	bad.UnitMs = 5
	if CheckBridge("http://127.0.0.1:9/action", bad) == nil {
		t.Error("unit_ms range")
	}
	if CheckBridge("http://user:pw@127.0.0.1:9/action", ok) == nil {
		t.Error("credentials in the URL")
	}
	_ = time.Second
}
