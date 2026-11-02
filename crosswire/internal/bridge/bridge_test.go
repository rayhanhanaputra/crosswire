package bridge

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/rig"
)

func serve(t *testing.T, kind string) (*httptest.Server, *Sim) {
	t.Helper()
	sim := &Sim{}
	s := NewWithBackend(Config{Kind: kind, DeviceID: "arduino-01", Resource: "pin-9", Simulate: true}, sim)
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return srv, sim
}

func post(t *testing.T, url, body string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

const ledOK = `{"device_id":"arduino-01","operation":"led_sequence","resource":"pin-9","duration_ms":5000,"unit_ms":200,"input":{"blink_units":[["on",1],["off",3],["on",3]]}}`

func TestLEDActionAndStop(t *testing.T) {
	srv, sim := serve(t, "led")
	code, out := post(t, srv.URL+"/action", ledOK)
	if code != 200 || out["accepted"] != true {
		t.Fatalf("%d %v", code, out)
	}
	if !sim.Active() || len(sim.Last().Pulses) != 3 || sim.Last().Pulses[2].Units != 3 || sim.Last().UnitMs != 200 {
		t.Errorf("%+v", sim.Last())
	}
	code, out = post(t, srv.URL+"/stop", `{"device_id":"arduino-01","operation":"led_sequence","resource":"pin-9","reason":"assessment_complete"}`)
	if code != 200 || out["stopped"] != true || sim.Active() {
		t.Fatalf("%d %v active=%v", code, out, sim.Active())
	}
	resp, _ := http.Get(srv.URL + "/health")
	var h map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&h)
	if h["simulate"] != true || h["resource"] != "pin-9" {
		t.Errorf("%v", h)
	}
}

func TestValidation(t *testing.T) {
	srv, sim := serve(t, "led")
	cases := map[string]string{
		"wrong device":    `{"device_id":"other","operation":"led_sequence","resource":"pin-9","input":{"blink_units":[["on",1]]}}`,
		"wrong resource":  `{"device_id":"arduino-01","operation":"led_sequence","resource":"pin-3","input":{"blink_units":[["on",1]]}}`,
		"wrong operation": `{"device_id":"arduino-01","operation":"ble_advertise","resource":"pin-9","input":{"blink_units":[["on",1]]}}`,
		"no steps":        `{"device_id":"arduino-01","operation":"led_sequence","resource":"pin-9","input":{"blink_units":[]}}`,
		"bad state":       `{"device_id":"arduino-01","operation":"led_sequence","resource":"pin-9","input":{"blink_units":[["dim",1]]}}`,
		"units too long":  `{"device_id":"arduino-01","operation":"led_sequence","resource":"pin-9","input":{"blink_units":[["on",21]]}}`,
		"unit_ms low":     `{"device_id":"arduino-01","operation":"led_sequence","resource":"pin-9","unit_ms":10,"input":{"blink_units":[["on",1]]}}`,
		"too long":        `{"device_id":"arduino-01","operation":"led_sequence","resource":"pin-9","duration_ms":500,"unit_ms":200,"input":{"blink_units":[["on",20]]}}`,
		"duration cap":    `{"device_id":"arduino-01","operation":"led_sequence","resource":"pin-9","duration_ms":99999,"input":{"blink_units":[["on",1]]}}`,
		"not json":        `nope`,
	}
	for name, body := range cases {
		if code, _ := post(t, srv.URL+"/action", body); code != 400 {
			t.Errorf("%s: want 400, got %d", name, code)
		}
	}
	if len(sim.Log()) != 0 {
		t.Errorf("a refused request must reach no backend: %v", sim.Log())
	}
	// A stop for a device that is not allowlisted is refused too.
	if code, _ := post(t, srv.URL+"/stop", `{"device_id":"other","operation":"led_sequence","resource":"pin-9"}`); code != 400 {
		t.Errorf("stop for another device: %d", code)
	}
}

func TestBLEBridgeNeedsInput(t *testing.T) {
	srv, sim := serve(t, "ble")
	body := `{"device_id":"arduino-01","operation":"ble_advertise","resource":"pin-9","duration_ms":3000,"input":{"advertisement_hex":"020106"}}`
	if code, _ := post(t, srv.URL+"/action", body); code != 200 || len(sim.Log()) != 1 {
		t.Errorf("ble action: %d %v", code, sim.Log())
	}
	if code, _ := post(t, srv.URL+"/action", `{"device_id":"arduino-01","operation":"ble_advertise","resource":"pin-9"}`); code != 400 {
		t.Errorf("no input: %d", code)
	}
}

func TestStopFailureIsAnError(t *testing.T) {
	srv, sim := serve(t, "led")
	sim.FailStop = true
	if code, _ := post(t, srv.URL+"/stop", `{"device_id":"arduino-01","operation":"led_sequence","resource":"pin-9"}`); code != 502 {
		t.Errorf("a failing backend stop must be 502, got %d", code)
	}
}

func TestExecBackend(t *testing.T) {
	// The helper is this test binary run as a tiny program through sh.
	e := &Exec{argv: []string{"sh", "-c", `cat >/dev/null; echo '{"ok":true,"verb":"'"$1"'"}'`, "helper"}}
	out, err := e.Start(context.Background(), Command{Operation: "ble_advertise"})
	if err != nil || out["verb"] != "start" {
		t.Fatalf("%v %v", err, out)
	}
	if out, err = e.Stop(context.Background(), Command{}); err != nil || out["verb"] != "stop" {
		t.Fatalf("%v %v", err, out)
	}
	bad := &Exec{argv: []string{"sh", "-c", `echo '{"ok":false,"error":"no adapter"}'`}}
	if _, err := bad.Start(context.Background(), Command{}); err == nil || !strings.Contains(err.Error(), "no adapter") {
		t.Errorf("%v", err)
	}
	junk := &Exec{argv: []string{"sh", "-c", `echo nonsense`}}
	if _, err := junk.Start(context.Background(), Command{}); err == nil {
		t.Error("non-JSON output")
	}
	fail := &Exec{argv: []string{"sh", "-c", `echo boom >&2; exit 3`}}
	if _, err := fail.Stop(context.Background(), Command{}); err == nil {
		t.Error("non-zero exit")
	}
}

func TestNewChoosesBackend(t *testing.T) {
	if _, _, err := New(Config{Kind: "led", DeviceID: "d", Resource: "r"}); err == nil {
		t.Error("no backend")
	}
	if _, _, err := New(Config{Kind: "ble", DeviceID: "d", Resource: "r", SerialPort: "/dev/null"}); err == nil {
		t.Error("serial backend only for led and gpio")
	}
	if _, _, err := New(Config{Kind: "radio", DeviceID: "d", Resource: "r", Simulate: true}); err == nil {
		t.Error("bad kind")
	}
	s, be, err := New(Config{Kind: "nfc", DeviceID: "d", Resource: "r", Simulate: true})
	if err != nil || s == nil {
		t.Fatal(err)
	}
	if _, ok := be.(*Sim); !ok {
		t.Error("simulate gives the Sim backend")
	}
}

// firmware stands in for the Arduino sketch on the other end of the serial port.
func firmware(t *testing.T, pin int) (io.ReadWriteCloser, *[]string) {
	hostR, boardW := io.Pipe()
	boardR, hostW := io.Pipe()
	var mu sync.Mutex
	var seen []string
	go func() {
		rd := bufio.NewReader(boardR)
		for {
			line, err := rd.ReadBytes('\n')
			if err != nil {
				return
			}
			var cmd map[string]any
			_ = json.Unmarshal(line, &cmd)
			mu.Lock()
			seen = append(seen, cmd["command"].(string))
			mu.Unlock()
			reply := `{"status":"ok","detail":"accepted"}`
			if int(cmd["pin"].(float64)) != pin {
				reply = `{"status":"error","detail":"pin_not_allowlisted"}`
			}
			_, _ = boardW.Write([]byte(reply + "\n"))
		}
	}()
	return struct {
		io.Reader
		io.Writer
		io.Closer
	}{hostR, hostW, hostW}, &seen
}

func TestSerialBackendDrivesTheRigThroughTheBridge(t *testing.T) {
	stream, seen := firmware(t, 9)
	be := NewSerial(rig.NewFromReadWriter(stream, 9))
	s := NewWithBackend(Config{Kind: "led", DeviceID: "arduino-01", Resource: "pin-9"}, be)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	if code, out := post(t, srv.URL+"/action", ledOK); code != 200 || out["accepted"] != true {
		t.Fatalf("%d %v", code, out)
	}
	if code, _ := post(t, srv.URL+"/stop", `{"device_id":"arduino-01","operation":"led_sequence","resource":"pin-9"}`); code != 200 {
		t.Fatalf("stop %d", code)
	}
	if strings.Join(*seen, ",") != "led_sequence,stop" {
		t.Errorf("firmware saw %v", *seen)
	}
}

func TestSerialBackendReportsAFirmwareRefusal(t *testing.T) {
	stream, _ := firmware(t, 2) // the board only allows pin 2
	be := NewSerial(rig.NewFromReadWriter(stream, 9))
	s := NewWithBackend(Config{Kind: "led", DeviceID: "arduino-01", Resource: "pin-9"}, be)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	if code, out := post(t, srv.URL+"/action", ledOK); code != 502 || !strings.Contains(out["error"].(string), "pin_not_allowlisted") {
		t.Errorf("%d %v", code, out)
	}
}

func TestMainRefusesANonLoopbackListenAddress(t *testing.T) {
	err := Main([]string{"--simulate", "--listen", "0.0.0.0:9910"})
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("the bridge has no authentication: %v", err)
	}
	if err := Main([]string{"--kind", "led"}); err == nil || !strings.Contains(err.Error(), "backend") {
		t.Fatalf("no backend chosen: %v", err)
	}
}
