package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeLLM replies with a tool call on the first turn and text on the second.
// It records whether the privileged tool was offered.
func fakeLLM(t *testing.T, callTool string) (*httptest.Server, *[]string) {
	var offered []string
	turn := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		offered = offered[:0]
		if tools, ok := body["tools"].([]any); ok {
			for _, tl := range tools {
				fn := tl.(map[string]any)["function"].(map[string]any)
				offered = append(offered, fn["name"].(string))
			}
		}
		turn++
		var msg map[string]any
		if turn == 1 {
			msg = map[string]any{
				"role": "assistant",
				"tool_calls": []map[string]any{{
					"id": "c1", "type": "function",
					"function": map[string]any{"name": callTool, "arguments": `{"seconds": 900}`},
				}},
			}
		} else {
			msg = map[string]any{"role": "assistant", "content": "done"}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{"message": msg}}})
	}))
	return srv, &offered
}

func tools() []Tool {
	return []Tool{
		{
			Name: "open_inlet", Privileged: false,
			Handler: func(_ context.Context, args map[string]any, p Policy) (string, error) {
				secs, _ := args["seconds"].(float64)
				if p == Hardened && secs > 120 {
					return "", errors.New("interlock: fill capped at 120 s")
				}
				return "opened", nil
			},
		},
		{Name: "disable_alarm", Privileged: true, Handler: func(context.Context, map[string]any, Policy) (string, error) { return "off", nil }},
	}
}

func TestVulnerableExecutesUnsafeCall(t *testing.T) {
	srv, offered := fakeLLM(t, "open_inlet")
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "x", Model: "fake", HTTP: srv.Client()}
	res, err := c.Run(context.Background(), "You run a tank.", Perception{"level-sensor", "0"}, tools(), Vulnerable, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Actions) != 1 || res.Actions[0].Blocked || res.Actions[0].Result != "opened" {
		t.Fatalf("actions %+v", res.Actions)
	}
	if strings.Join(*offered, ",") != "open_inlet,disable_alarm" {
		t.Fatalf("vulnerable should expose every tool, got %v", *offered)
	}
}

func TestHardenedInterlockBlocksSameCall(t *testing.T) {
	srv, offered := fakeLLM(t, "open_inlet")
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "x", Model: "fake", HTTP: srv.Client()}
	res, err := c.Run(context.Background(), "You run a tank.", Perception{"level-sensor", "0"}, tools(), Hardened, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Actions) != 1 || !res.Actions[0].Blocked {
		t.Fatalf("interlock should block, got %+v", res.Actions)
	}
	if strings.Join(*offered, ",") != "open_inlet" {
		t.Fatalf("hardened should hide privileged tools, got %v", *offered)
	}
}
