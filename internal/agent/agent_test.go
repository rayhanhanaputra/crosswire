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

func TestRefusalIsCarriedAsActionError(t *testing.T) {
	srv, _ := fakeLLM(t, "open_inlet")
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "x", Model: "fake", HTTP: srv.Client()}
	res, err := c.Run(context.Background(), "You run a tank.", Perception{"level-sensor", "0"}, tools(), Hardened, 3)
	if err != nil {
		t.Fatal(err)
	}
	a := res.Actions[0]
	if !a.Blocked || a.Error == "" || a.Result != "refused: "+a.Error {
		t.Fatalf("action %+v", a)
	}
}

func TestRunPromptHiddenReachesHiddenHandlerButOffersOnlyShown(t *testing.T) {
	srv, offered := fakeLLM(t, "disable_alarm")
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "x", Model: "fake", HTTP: srv.Client()}
	var called bool
	hidden := []Tool{{Name: "disable_alarm", Privileged: true, Handler: func(ctx context.Context, a map[string]any, p Policy) (string, error) {
		called = true
		return "", errors.New("withheld")
	}}}
	shown := []Tool{{Name: "open_inlet", Handler: func(ctx context.Context, a map[string]any, p Policy) (string, error) { return "ok", nil }}}
	res, err := c.RunPromptHidden(context.Background(), "sys", "user", shown, hidden, Hardened, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !called || len(res.Actions) != 1 || res.Actions[0].Error != "withheld" {
		t.Fatalf("called=%v actions=%+v", called, res.Actions)
	}
	if strings.Join(*offered, ",") != "open_inlet" {
		t.Fatalf("offered %v", *offered)
	}
}

func TestTemperatureFromEnvAndSentOnTheWire(t *testing.T) {
	t.Setenv("CROSSWIRE_LLM_API_KEY", "k")
	t.Setenv("CROSSWIRE_LLM_TEMPERATURE", "0.7")
	c, err := ClientFromEnv()
	if err != nil || c.Temperature != 0.7 {
		t.Fatalf("%v %+v", err, c)
	}
	var got float64 = -1
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Temperature float64 `json:"temperature"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		got = body.Temperature
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": "ALLOW"}}}})
	}))
	defer srv.Close()
	c.BaseURL, c.HTTP = srv.URL, srv.Client()
	if _, err := c.Complete(context.Background(), "s", "u", "", false); err != nil || got != 0.7 {
		t.Fatalf("Complete sent %v err %v", got, err)
	}
	got = -1
	if _, err := c.RunPrompt(context.Background(), "s", "u", nil, Vulnerable, 1); err != nil || got != 0.7 {
		t.Fatalf("RunPrompt sent %v err %v", got, err)
	}
	t.Setenv("CROSSWIRE_LLM_TEMPERATURE", "hot")
	if _, err := ClientFromEnv(); err == nil {
		t.Fatal("bad temperature must fail")
	}
	t.Setenv("CROSSWIRE_LLM_TEMPERATURE", "")
	if c, _ := ClientFromEnv(); c.Temperature != 0 {
		t.Fatalf("default temperature %v", c.Temperature)
	}
}

func TestClientFromConfigUsesNamedKeyAndBaseURL(t *testing.T) {
	t.Setenv("CROSSWIRE_LLM_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "openai-secret")
	t.Setenv("PROVIDER_KEY", "provider-secret")
	c, err := ClientFromConfig("https://api.example.test/v1/", "PROVIDER_KEY")
	if err != nil || c.APIKey != "provider-secret" || c.BaseURL != "https://api.example.test/v1" {
		t.Fatalf("%v %+v", err, c)
	}
	t.Setenv("PROVIDER_KEY", "")
	_, err = ClientFromConfig("https://api.example.test/v1", "PROVIDER_KEY")
	if err == nil || !strings.Contains(err.Error(), "PROVIDER_KEY") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("error must name the variable and never a key: %v", err)
	}
	if c, err := ClientFromConfig("", ""); err != nil || c.APIKey != "openai-secret" || c.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("env fallback broke: %v %+v", err, c)
	}
}
