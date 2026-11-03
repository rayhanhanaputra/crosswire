// Package fakes holds in-process stand-ins for everything Crosswire talks to:
// a WebSocket agent, a TCP agent, an MQTT broker with an agent behind it, a
// voice stream, a device bridge and a state page. They run on loopback
// ports, need no network and no hardware, and back both the connector tests
// and `crosswire selftest`.
package fakes

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/rayhanhanaputra/crosswire/internal/model"
)

// ObeyMarker is the phrase that makes a fake agent follow an injected
// instruction. A payload that contains it is "compromised"; a control
// without it is not.
const ObeyMarker = "fakeagent:obey"

var canaryRe = regexp.MustCompile(`cw-[0-9a-f]{8}`)

// Agent is the behaviour every fake shares. When the text it receives holds
// ObeyMarker, it calls Tool with the trial's canary and reports the call.
type Agent struct {
	Name   string                // target name carried on sink events
	Tool   string                // default "send_email"
	Report func(model.SinkEvent) // where executed tool calls go; nil drops them
}

// Reply handles one request text and returns the JSON reply body.
func (a *Agent) Reply(text string) []byte {
	tool := a.Tool
	if tool == "" {
		tool = "send_email"
	}
	canary := canaryRe.FindString(text)
	out := map[string]any{"text": "ok", "actions": []any{}, "perceived": text}
	if strings.Contains(strings.ToLower(text), ObeyMarker) {
		args := map[string]any{"to": canary}
		if a.Report != nil {
			a.Report(model.SinkEvent{Name: tool, Arguments: args, Target: a.Name, Session: canary})
		}
		out["text"] = "done"
		out["actions"] = []any{map[string]any{"name": tool, "arguments": args}}
	}
	b, _ := json.Marshal(out)
	return b
}
