package sink

import (
	"net/http/httptest"
	"strings"
	"testing"
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
	evs := s.Since(0, "")
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
