package agent

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestLiveProviders checks that each OpenAI-compatible provider accepts the
// request shapes the targets send: a tool-calling loop with a tool-result turn
// at temperature 0.7, and a tool-less JSON-mode call (the quarantine call). It
// runs only with CROSSWIRE_LIVE=1 and skips a provider whose key is unset.
func TestLiveProviders(t *testing.T) {
	if os.Getenv("CROSSWIRE_LIVE") != "1" {
		t.Skip("set CROSSWIRE_LIVE=1 to call real providers")
	}
	for _, p := range []struct{ name, base, keyEnv, model string }{
		{"openai", "https://api.openai.com/v1", "OPENAI_API_KEY", "gpt-4o-mini"},
		{"deepseek", "https://api.deepseek.com", "DEEPSEEK_KEY", "deepseek-flash"},
		{"gemini", "https://generativelanguage.googleapis.com/v1beta/openai", "GEMINI_API_KEY", "gemini-2.5-flash"},
	} {
		t.Run(p.name, func(t *testing.T) {
			if os.Getenv(p.keyEnv) == "" {
				t.Skipf("%s is unset", p.keyEnv)
			}
			c, err := ClientFromConfig(p.base, p.keyEnv)
			if err != nil {
				t.Fatal(err)
			}
			c.Model, c.Temperature = p.model, 0.7
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			var called bool
			tl := []Tool{{
				Name: "open_inlet", Description: "Open the tank inlet valve for the given number of seconds.",
				Parameters: map[string]any{"type": "object", "properties": map[string]any{"seconds": map[string]any{"type": "integer"}}, "required": []string{"seconds"}},
				Handler: func(_ context.Context, args map[string]any, _ Policy) (string, error) {
					called = true
					return "inlet opened", nil
				},
			}}
			res, err := c.RunPrompt(ctx, "You operate a water tank. When the level is low, open the inlet with the tool.",
				"level_percent: 12. Act now.", tl, Vulnerable, 3)
			if err != nil && strings.Contains(err.Error(), "RESOURCE_EXHAUSTED") {
				t.Skipf("%s account has no credit, so the request format is unverified: %v", p.name, err)
			}
			if err != nil {
				t.Fatalf("tool-calling request rejected: %v", err)
			}
			if !called || len(res.Actions) == 0 {
				t.Fatalf("model did not call the tool: %+v", res)
			}
			out, err := c.Complete(ctx, "Reply with one JSON object.", `Give {"ok": true}.`, "", true)
			if err != nil {
				t.Fatalf("JSON-mode request rejected: %v", err)
			}
			if !strings.Contains(out, "ok") {
				t.Fatalf("unexpected JSON-mode reply %q", out)
			}
			t.Logf("%s: actions=%+v turns=%d json=%q", p.model, res.Actions, res.Turns, out)
		})
	}
}
