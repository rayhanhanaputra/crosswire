package connector

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/model"
)

func TestRenderEscapesPayload(t *testing.T) {
	tgt := model.Target{RequestTemplate: `{"input":"{{content}}","meta":{"src":"{{source}}"}}`}
	body, err := Render(tgt, Input{Source: "qr", Content: `say "hi" \ and {break}`})
	if err != nil {
		t.Fatal(err)
	}
	if body["input"] != `say "hi" \ and {break}` {
		t.Fatalf("got %q", body["input"])
	}
}

func TestSendAndParse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Key") != "secret-value" {
			w.WriteHeader(401)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result":  map[string]any{"text": "ok, opening"},
			"actions": []map[string]any{{"name": "open_inlet", "arguments": map[string]any{"seconds": 900}}},
		})
	}))
	defer srv.Close()
	t.Setenv("TEST_TARGET_KEY", "secret-value")
	tgt := model.Target{
		Endpoint:      srv.URL,
		SecretHeaders: map[string]string{"X-Key": "TEST_TARGET_KEY"},
		ResponsePath:  "result.text",
		ActionsPath:   "actions",
	}
	ex, err := Send(context.Background(), tgt, Perception, Input{Source: "sensor", Content: "0"})
	if err != nil {
		t.Fatal(err)
	}
	if ex.ResponseText != "ok, opening" || len(ex.Actions) != 1 || ex.Actions[0].Name != "open_inlet" {
		t.Fatalf("exchange %+v", ex)
	}
	if ex.Actions[0].Arguments["seconds"].(float64) != 900 {
		t.Fatalf("arguments %+v", ex.Actions[0].Arguments)
	}
}

func TestDirectArmUsesChatEndpoint(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{"text": "hi", "perceived": "cw-9 seen"})
	}))
	defer srv.Close()
	tgt := model.Target{Endpoint: srv.URL + "/perceive", DirectEndpoint: srv.URL + "/chat"}
	ex, err := Send(context.Background(), tgt, Direct, Input{Content: "do it cw-9", Canary: "cw-9"})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/chat" || gotBody["message"] != "do it cw-9" || ex.Perceived != "cw-9 seen" {
		t.Fatalf("path %s body %v perceived %q", gotPath, gotBody, ex.Perceived)
	}
	if _, err := Send(context.Background(), model.Target{Endpoint: srv.URL}, Direct, Input{Content: "x"}); err == nil {
		t.Fatal("missing direct endpoint must fail")
	}
}

func TestRedirectRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.1:1/", http.StatusFound)
	}))
	defer srv.Close()
	_, err := Send(context.Background(), model.Target{Endpoint: srv.URL}, Perception, Input{Content: "x"})
	if err == nil {
		t.Fatal("redirect should fail")
	}
}

func TestParseCarriesResultErrorAndBlocked(t *testing.T) {
	raw := `{"text":"x","actions":[
		{"name":"a","arguments":{},"result":"done"},
		{"name":"b","arguments":{},"blocked":true,"error":"needs user confirmation","result":"refused: needs user confirmation"}]}`
	_, acts, _ := Parse(raw, "", "", "")
	if len(acts) != 2 || acts[0].Result != "done" || acts[0].Blocked || !acts[1].Blocked || acts[1].Error != "needs user confirmation" {
		t.Fatalf("%+v", acts)
	}
}
