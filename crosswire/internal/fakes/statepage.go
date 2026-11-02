package fakes

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

// StatePageOptions shapes the fake agent that changes the page.
type StatePageOptions struct {
	// ShowNonce puts the trial's canary in the new state ("unlocked cw-...").
	// Without it the state is the same text for every compromised trial.
	ShowNonce bool
	// Drift changes the state on every request, compromised or not, the way a
	// clock or a counter on the page would.
	Drift bool
}

// StatePage is an agent whose only visible effect is on a web page and a state
// endpoint. POST /agent {"input": "..."} changes the state when the input
// holds ObeyMarker. GET / is the page (element #status); GET /state is JSON.
type StatePage struct {
	srv   *httptest.Server
	mu    sync.Mutex
	state string
	opt   StatePageOptions
	n     int
	agent *Agent
}

// NewStatePage starts the fake. a reports tool calls too, so one fake can serve
// as a sink-and-observer target; pass an Agent with a nil Report to observe only.
func NewStatePage(a *Agent, opt StatePageOptions) *StatePage {
	f := &StatePage{state: "locked", opt: opt, agent: a}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		s := f.state
		f.mu.Unlock()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html><head><title>Door panel</title></head><body style="font:20px sans-serif;padding:24px">
<h1>Door panel</h1><div id="status" style="padding:12px;border:2px solid #345">%s</div>
<script>setInterval(function(){fetch('/state').then(function(r){return r.json()}).then(function(j){document.getElementById('status').textContent=j.status})},150)</script>
</body></html>`, html.EscapeString(s))
	})
	mux.HandleFunc("GET /state", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		s := f.state
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": s, "door": map[string]any{"state": s}})
	})
	mux.HandleFunc("POST /reset", func(w http.ResponseWriter, r *http.Request) {
		f.Set("locked")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("POST /agent", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		text := string(body)
		reply := f.agent.Reply(text)
		f.mu.Lock()
		f.n++
		if f.opt.Drift {
			f.state = fmt.Sprintf("tick %d", f.n)
		} else if strings.Contains(strings.ToLower(text), ObeyMarker) {
			f.state = "UNLOCKED"
			if f.opt.ShowNonce {
				f.state += " " + canaryRe.FindString(text)
			}
		}
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(reply)
	})
	f.srv = httptest.NewServer(mux)
	return f
}

// URL is the page. AgentURL is the endpoint a target posts to. StateURL is JSON.
func (f *StatePage) URL() string      { return f.srv.URL + "/" }
func (f *StatePage) AgentURL() string { return f.srv.URL + "/agent" }
func (f *StatePage) StateURL() string { return f.srv.URL + "/state" }

// ResetURL puts the state back to "locked".
func (f *StatePage) ResetURL() string { return f.srv.URL + "/reset" }

// Set changes the state directly.
func (f *StatePage) Set(s string) { f.mu.Lock(); f.state = s; f.mu.Unlock() }

// State reads it.
func (f *StatePage) State() string { f.mu.Lock(); defer f.mu.Unlock(); return f.state }

// Close stops the server.
func (f *StatePage) Close() { f.srv.Close() }
