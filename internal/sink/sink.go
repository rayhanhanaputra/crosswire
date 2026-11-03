// Package sink receives out-of-band tool executions from targets. A tool call
// that reaches the sink with a trial's canary is the only thing the evidence
// engine counts as a hit.
package sink

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rayhanhanaputra/crosswire/internal/model"
)

const capacity = 10000

// Sink is an in-memory ring of events.
type Sink struct {
	mu     sync.Mutex
	events []model.SinkEvent
	next   int64
}

// New creates an empty sink.
func New() *Sink { return &Sink{} }

// Add records an event and returns it with its id.
func (s *Sink) Add(ev model.SinkEvent) model.SinkEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	ev.ID = s.next
	if ev.Received.IsZero() {
		ev.Received = time.Now()
	}
	s.events = append(s.events, ev)
	if len(s.events) > capacity {
		s.events = s.events[len(s.events)-capacity:]
	}
	return ev
}

// Cursor returns the id of the newest event; events after it are "new".
func (s *Sink) Cursor() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.next
}

// Since returns events with id > cursor, optionally only those whose
// arguments contain needle (case-insensitive). A non-empty session or target
// keeps only events of that session and that target name; an event that
// names neither (older or third-party targets) is kept, as before.
func (s *Sink) Since(cursor int64, needle, session, target string) []model.SinkEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []model.SinkEvent
	needle = strings.ToLower(needle)
	for _, ev := range s.events {
		if ev.ID <= cursor {
			continue
		}
		// An event that names a session belongs to that session only. Events
		// without one (older or third-party targets) are kept, as before.
		if session != "" && ev.Session != "" && ev.Session != session {
			continue
		}
		if target != "" && ev.Target != "" && ev.Target != target {
			continue
		}
		if needle != "" {
			args, _ := json.Marshal(ev.Arguments)
			if !strings.Contains(strings.ToLower(string(args)), needle) {
				continue
			}
		}
		out = append(out, ev)
	}
	return out
}

// Recent returns the newest n events for the UI.
func (s *Sink) Recent(n int) []model.SinkEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n <= 0 || n > len(s.events) {
		n = len(s.events)
	}
	out := make([]model.SinkEvent, n)
	copy(out, s.events[len(s.events)-n:])
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// Handler accepts POST {name, arguments, target, blocked, reason, layers}.
func (s *Sink) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			Name      string          `json:"name"`
			Arguments map[string]any  `json:"arguments"`
			Target    string          `json:"target"`
			Blocked   bool            `json:"blocked"`
			Reason    string          `json:"reason"`
			Layers    map[string]bool `json:"layers"`
			Session   string          `json:"session"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil || body.Name == "" {
			http.Error(w, "expected {name, arguments}", http.StatusBadRequest)
			return
		}
		ev := s.Add(model.SinkEvent{Name: body.Name, Arguments: body.Arguments, Target: body.Target,
			Blocked: body.Blocked, Reason: body.Reason, Layers: body.Layers, Session: body.Session})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": ev.ID})
	}
}
