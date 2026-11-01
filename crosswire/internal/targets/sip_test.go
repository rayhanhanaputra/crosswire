package targets

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/agent"
	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/connector"
	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/model"
	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/signal"
)

func TestSIPCallRunsThePerceivePath(t *testing.T) {
	c := cases()[1] // voice-agent: ASR returns text, the fake model then calls send_email
	f := c.fake()
	r := newRigWith(t, c.name, f, Options{Policy: agent.Vulnerable})
	a, err := r.s.SIPAnswerer("127.0.0.1:0", false)
	if err != nil {
		t.Fatal(err)
	}
	a.Idle = 500 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	go a.Serve(ctx)

	uri := fmt.Sprintf("sip:voice@127.0.0.1:%d", a.Addr().Port)
	wav := signal.WAV(16000, signal.Tone(16000, 16000, 0.5, 440)) // one second
	call, err := connector.Dial(ctx, model.SIPConfig{LocalIP: "127.0.0.1", ListenSeconds: 15}, uri, wav)
	if err != nil {
		t.Fatalf("%v\n%s", err, strings.Join(call.Log, "\n"))
	}
	if !call.Answered || !call.RemoteHungUp {
		t.Fatalf("the listener must answer, run the agent and hang up: %+v", call)
	}
	f.mu.Lock()
	asr := f.asrCalls
	f.mu.Unlock()
	if asr != 1 {
		t.Fatalf("caller audio must reach ASR once, got %d", asr)
	}
	ev := r.sinkEvents()
	if len(ev) != 1 || ev[0]["name"] != "send_email" || ev[0]["blocked"] != false || !strings.Contains(jsonString(ev[0]["arguments"]), "cw-aaaa1111") {
		t.Fatalf("the agent's tool call must reach the sink: %v", ev)
	}
}

func TestSIPAnswererOnlyOnVoiceAgent(t *testing.T) {
	c := cases()[0]
	r := newRig(t, c.name, agent.Vulnerable, false, c.fake())
	if _, err := r.s.SIPAnswerer("127.0.0.1:0", false); err == nil {
		t.Fatal("want an error for a non-voice target")
	}
}
