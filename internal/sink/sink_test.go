package sink

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rayhanhanaputra/crosswire/internal/model"
)

func TestHandlerKeepsBlockedReasonAndLayers(t *testing.T) {
	s := New()
	post := func(body string) int {
		rec := httptest.NewRecorder()
		s.Handler()(rec, httptest.NewRequest("POST", "/api/sink", strings.NewReader(body)))
		return rec.Code
	}
	if c := post(`{"name":"send_email","arguments":{"to":"x"},"target":"voice-agent","blocked":true,"reason":"needs user confirmation","layers":{"confirm":true,"withhold":false}}`); c != 200 {
		t.Fatalf("status %d", c)
	}
	if c := post(`{"name":"set_reminder","arguments":{}}`); c != 200 {
		t.Fatalf("status %d", c)
	}
	evs := s.Since(0, "", "", "")
	if len(evs) != 2 {
		t.Fatalf("events %+v", evs)
	}
	if !evs[0].Blocked || evs[0].Reason != "needs user confirmation" || !evs[0].Layers["confirm"] || evs[0].Layers["withhold"] {
		t.Fatalf("blocked event %+v", evs[0])
	}
	if evs[1].Blocked || evs[1].Reason != "" {
		t.Fatalf("an event without the field is an executed call: %+v", evs[1])
	}
}

func TestHandlerKeepsSession(t *testing.T) {
	s := New()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/sink", strings.NewReader(`{"name":"issue_refund","arguments":{},"session":"cw-a"}`))
	s.Handler().ServeHTTP(rec, req)
	if got := s.Since(0, "", "cw-a", ""); len(got) != 1 || got[0].Session != "cw-a" {
		t.Fatalf("session not kept: %+v (status %d)", got, rec.Code)
	}
}

func TestSinceKeepsOnlyTheTrialSession(t *testing.T) {
	s := New()
	s.Add(model.SinkEvent{Name: "issue_refund", Session: "cw-a"})
	s.Add(model.SinkEvent{Name: "issue_refund", Session: "cw-b"})
	s.Add(model.SinkEvent{Name: "issue_refund"}) // no session: kept for any trial
	got := s.Since(0, "", "cw-a", "")
	if len(got) != 2 || got[0].Session != "cw-a" || got[1].Session != "" {
		t.Fatalf("want the cw-a event and the unlabelled one, got %+v", got)
	}
	if all := s.Since(0, "", "", ""); len(all) != 3 {
		t.Fatalf("no session filter should keep all three, got %d", len(all))
	}
}

func TestSinceKeepsOnlyTheNamedTarget(t *testing.T) {
	s := New()
	s.Add(model.SinkEvent{Name: "send_email", Target: "voice-agent", Session: "cw-a"})
	s.Add(model.SinkEvent{Name: "approve_payment", Target: "expense-agent", Session: "cw-a"})
	s.Add(model.SinkEvent{Name: "disarm_alarm", Target: "camera-agent"}) // no session, other target
	s.Add(model.SinkEvent{Name: "send_email", Target: "voice-agent", Session: "cw-b"})
	got := s.Since(0, "", "cw-a", "voice-agent")
	if len(got) != 1 || got[0].Name != "send_email" || got[0].Session != "cw-a" {
		t.Fatalf("want only the voice-agent cw-a event, got %+v", got)
	}
	if all := s.Since(0, "", "", ""); len(all) != 4 {
		t.Fatalf("no filter should keep all four, got %d", len(all))
	}
}
