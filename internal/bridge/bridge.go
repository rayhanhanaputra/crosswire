// Package bridge is the HTTP device bridge: the small service a bridge target
// talks to. It accepts one allowlisted device and resource, validates every
// request, and drives the stimulus through a backend:
//
//   - simulate: records the command, touches nothing (tests, dry runs)
//   - serial:   the Arduino/ESP32 LED rig over a serial port (kind led or gpio)
//   - exec:     an external helper program you provide (BLE, NFC, GPIO), so the
//     crosswire binary needs no platform Bluetooth or NFC library
//
// The protocol is POST /action, POST /stop, GET /health.
package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/rayhanhanaputra/crosswire/internal/model"
	"github.com/rayhanhanaputra/crosswire/internal/rig"
	"github.com/rayhanhanaputra/crosswire/internal/signal"
)

const (
	maxBody     = 64 << 10
	maxSteps    = 256
	maxDuration = 30000
)

// Config describes the one device the bridge exposes.
type Config struct {
	Kind     string // ble | nfc | gpio | led
	DeviceID string
	Resource string
	Pin      int
	// Backend selection. Simulate wins; then SerialPort (led, gpio); then Exec.
	Simulate   bool
	SerialPort string
	Exec       []string // helper command and arguments
}

// Command is one validated request handed to a backend.
type Command struct {
	Operation  string         `json:"operation"`
	DeviceID   string         `json:"device_id"`
	Resource   string         `json:"resource"`
	DurationMs int            `json:"duration_ms"`
	UnitMs     int            `json:"unit_ms,omitempty"`
	Input      map[string]any `json:"input"`
	Pulses     []signal.Pulse `json:"-"`
}

// Backend carries a command out.
type Backend interface {
	Start(ctx context.Context, c Command) (map[string]any, error)
	Stop(ctx context.Context, c Command) (map[string]any, error)
}

// Server validates requests and calls the backend.
type Server struct {
	cfg Config
	be  Backend
	mu  sync.Mutex
}

// New builds a server around a backend chosen from the config.
func New(cfg Config) (*Server, Backend, error) {
	if model.BridgeOperation(cfg.Kind) == "" {
		return nil, nil, errors.New("kind must be ble, nfc, gpio or led")
	}
	if cfg.DeviceID == "" || cfg.Resource == "" {
		return nil, nil, errors.New("device id and resource are required")
	}
	var be Backend
	switch {
	case cfg.Simulate:
		be = &Sim{}
	case cfg.SerialPort != "":
		if cfg.Kind != "led" && cfg.Kind != "gpio" {
			return nil, nil, errors.New("the serial backend drives the LED rig: use kind led or gpio")
		}
		r, err := rig.Open(cfg.SerialPort, cfg.Pin)
		if err != nil {
			return nil, nil, err
		}
		be = &Serial{rig: r}
	case len(cfg.Exec) > 0:
		be = &Exec{argv: cfg.Exec}
	default:
		return nil, nil, errors.New("choose a backend: --simulate, --serial-port or --exec")
	}
	return &Server{cfg: cfg, be: be}, be, nil
}

// NewWithBackend is New with a backend you made, for tests.
func NewWithBackend(cfg Config, be Backend) *Server { return &Server{cfg: cfg, be: be} }

// Handler serves /health, /action and /stop.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		_, sim := s.be.(*Sim)
		writeJSON(w, 200, map[string]any{"status": "ok", "kind": s.cfg.Kind, "device_id": s.cfg.DeviceID, "resource": s.cfg.Resource, "simulate": sim})
	})
	mux.HandleFunc("POST /action", func(w http.ResponseWriter, r *http.Request) { s.handle(w, r, true) })
	mux.HandleFunc("POST /stop", func(w http.ResponseWriter, r *http.Request) { s.handle(w, r, false) })
	return mux
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request, start bool) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil || len(body) == 0 || len(body) > maxBody {
		writeJSON(w, 400, map[string]string{"error": "request body must be 1 to 65536 bytes"})
		return
	}
	var req struct {
		Command
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "request body must be a JSON object"})
		return
	}
	cmd := req.Command
	if cmd.DeviceID != s.cfg.DeviceID {
		writeJSON(w, 400, map[string]string{"error": "device_id is not allowlisted"})
		return
	}
	if cmd.Resource != s.cfg.Resource {
		writeJSON(w, 400, map[string]string{"error": "resource is not allowlisted"})
		return
	}
	if want := model.BridgeOperation(s.cfg.Kind); cmd.Operation != want {
		writeJSON(w, 400, map[string]string{"error": "only " + want + " is supported"})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 20*time.Second)
	defer cancel()
	if !start {
		out, err := s.be.Stop(ctx, cmd)
		if err != nil {
			writeJSON(w, 502, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"stopped": true, "device_id": s.cfg.DeviceID, "controller": out})
		return
	}
	if err := s.validateStart(&cmd); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	out, err := s.be.Start(ctx, cmd)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"accepted": true, "device_id": s.cfg.DeviceID, "resource": s.cfg.Resource, "duration_ms": cmd.DurationMs, "controller": out})
}

func (s *Server) validateStart(c *Command) error {
	if c.DurationMs <= 0 {
		c.DurationMs = maxDuration
	}
	if c.DurationMs > maxDuration {
		return fmt.Errorf("duration_ms is limited to %d", maxDuration)
	}
	if s.cfg.Kind != "led" && s.cfg.Kind != "gpio" {
		if len(c.Input) == 0 {
			return errors.New("input is required")
		}
		return nil
	}
	if c.UnitMs == 0 {
		c.UnitMs = 200
	}
	if c.UnitMs < 50 || c.UnitMs > 2000 {
		return errors.New("unit_ms must be an integer from 50 to 2000")
	}
	raw, ok := c.Input["blink_units"].([]any)
	if !ok || len(raw) < 1 || len(raw) > maxSteps {
		return fmt.Errorf("input.blink_units must hold 1 to %d steps", maxSteps)
	}
	total := 0
	for _, item := range raw {
		step, ok := item.([]any)
		if !ok || len(step) != 2 {
			return errors.New("each blink step must be [state, units]")
		}
		state, _ := step[0].(string)
		units, isNum := step[1].(float64)
		if state != "on" && state != "off" {
			return errors.New("blink state must be on or off")
		}
		if !isNum || units < 1 || units > 20 || units != float64(int(units)) {
			return errors.New("blink units must be an integer from 1 to 20")
		}
		c.Pulses = append(c.Pulses, signal.Pulse{On: state == "on", Units: int(units)})
		total += int(units) * c.UnitMs
	}
	if total > c.DurationMs {
		return errors.New("the sequence exceeds the configured duration limit")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Sim records commands and drives nothing.
type Sim struct {
	mu     sync.Mutex
	log    []string
	active bool
	last   Command
	// FailStop makes Stop return an error.
	FailStop bool
	// OnStart, when set, is called with every started command. Tests use it to
	// stand in for the agent that reacts to the stimulus.
	OnStart func(Command)
}

// Start records the command and marks the stimulus active.
func (s *Sim) Start(_ context.Context, c Command) (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log, s.active, s.last = append(s.log, "start"), true, c
	if s.OnStart != nil {
		s.OnStart(c)
	}
	return map[string]any{"status": "ok", "simulated": true}, nil
}

// Stop records the stop and marks the stimulus off.
func (s *Sim) Stop(_ context.Context, _ Command) (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log = append(s.log, "stop")
	if s.FailStop {
		return nil, errors.New("simulated stop failure")
	}
	s.active = false
	return map[string]any{"status": "ok", "simulated": true}, nil
}

// Log lists "start" and "stop" in order.
func (s *Sim) Log() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.log...)
}

// Active is true between a start and a stop.
func (s *Sim) Active() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.active }

// Last is the most recent started command.
func (s *Sim) Last() Command { s.mu.Lock(); defer s.mu.Unlock(); return s.last }

// Serial drives the Arduino/ESP32 LED rig. Real hardware is untested; the tests
// run it against a simulated firmware on an in-memory stream.
type Serial struct{ rig *rig.Rig }

// NewSerial wraps an open rig.
func NewSerial(r *rig.Rig) *Serial { return &Serial{rig: r} }

// Start plays the pulse timeline; the firmware's watchdog bounds it.
func (s *Serial) Start(_ context.Context, c Command) (map[string]any, error) {
	reply, err := s.rig.Play(c.Pulses, c.UnitMs, min(c.DurationMs, maxDuration))
	if err != nil {
		return nil, err
	}
	return map[string]any{"status": reply.Status, "detail": reply.Detail}, nil
}

// Stop sends the rig's stop command.
func (s *Serial) Stop(_ context.Context, _ Command) (map[string]any, error) {
	reply, err := s.rig.Stop()
	if err != nil {
		return nil, err
	}
	return map[string]any{"status": reply.Status, "detail": reply.Detail}, nil
}

// Exec runs an external helper. The helper is called as
//
//	HELPER start   (request JSON on stdin)
//	HELPER stop    (request JSON on stdin)
//
// and must print one JSON object on stdout with "ok": true, exit 0, and return
// within 20 s. For a continuous stimulus (BLE advertising, NFC emulation) the
// helper's start must begin it and return, and its stop must end it.
type Exec struct{ argv []string }

// Start runs the helper's start.
func (e *Exec) Start(ctx context.Context, c Command) (map[string]any, error) {
	return e.run(ctx, "start", c)
}

// Stop runs the helper's stop.
func (e *Exec) Stop(ctx context.Context, c Command) (map[string]any, error) {
	return e.run(ctx, "stop", c)
}

func (e *Exec) run(ctx context.Context, verb string, c Command) (map[string]any, error) {
	in, _ := json.Marshal(c)
	args := append(append([]string(nil), e.argv[1:]...), verb)
	cmd := exec.CommandContext(ctx, e.argv[0], args...)
	cmd.Stdin = bytes.NewReader(in)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("helper %s failed: %v %s", verb, err, strings.TrimSpace(tail(errb.String(), 300)))
	}
	var reply map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &reply); err != nil {
		return nil, fmt.Errorf("helper %s did not print one JSON object", verb)
	}
	if ok, _ := reply["ok"].(bool); !ok {
		return nil, fmt.Errorf("helper %s reported failure: %v", verb, reply["error"])
	}
	return reply, nil
}

func tail(s string, n int) string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}
