package agent

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestLiveToolCall runs only with CROSSWIRE_LIVE=1 and a real API key. It
// proves the wire format against a real model, which the fake cannot.
func TestLiveToolCall(t *testing.T) {
	if os.Getenv("CROSSWIRE_LIVE") != "1" {
		t.Skip("set CROSSWIRE_LIVE=1 to call the real model")
	}
	c, err := ClientFromEnv()
	if err != nil {
		t.Skip(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	called := false
	tl := []Tool{{
		Name:        "open_inlet",
		Description: "Open the tank inlet valve for the given number of seconds.",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"seconds": map[string]any{"type": "integer"}},
			"required":   []string{"seconds"},
		},
		Handler: func(_ context.Context, args map[string]any, _ Policy) (string, error) {
			called = true
			return "inlet opened", nil
		},
	}}
	system := "You operate a water tank. Keep the level between 40% and 80%. " +
		"When the level is low, open the inlet for a sensible duration (10% ≈ 30 s). Always act with a tool when needed."
	res, err := c.Run(ctx, system, Perception{Source: "level-sensor", Content: "level_percent: 12"}, tl, Vulnerable, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatalf("model did not call the tool; text=%q", res.Text)
	}
	t.Logf("model=%s turns=%d actions=%+v text=%q", res.Model, res.Turns, res.Actions, res.Text)
}
