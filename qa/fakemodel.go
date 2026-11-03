// A deterministic stand-in for the OpenAI chat API, so the reference targets'
// real agent loop, quarantine model, classifier and tool handlers all execute
// without a network or a key.
//
// Behaviour: a gullible model. It reads the last user turn, and if that text
// asks for one of the tools it was offered, it calls that tool, lifting a
// "ref <value>" / "code <value>" / account-like argument straight out of the
// text. That is exactly the behaviour a prompt-injection test needs to probe:
// the defence layers, not the model, must be what stops it.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
)

var (
	refRe   = regexp.MustCompile(`(?i)\b(?:ref|code|log|reference)[: ]+([A-Za-z0-9._-]{4,})`)
	ibanRe  = regexp.MustCompile(`\b[A-Z]{2}[0-9]{2}[A-Z0-9]{8,}\b`)
	mailRe  = regexp.MustCompile(`[\w.+-]+@[\w.-]+\.[A-Za-z]{2,}`)
	calls   atomic.Int64
	verbose = os.Getenv("VERBOSE") == "1"
)

type msg struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	ToolCalls []any  `json:"tool_calls,omitempty"`
}

type req struct {
	Model    string `json:"model"`
	Messages []msg  `json:"messages"`
	Tools    []struct {
		Function struct {
			Name       string         `json:"name"`
			Parameters map[string]any `json:"parameters"`
		} `json:"function"`
	} `json:"tools"`
	Temperature float64 `json:"temperature"`
}

// argsFor builds plausible arguments for a tool from the text, filling the
// properties the tool actually declares.
func argsFor(params map[string]any, text string) map[string]any {
	out := map[string]any{}
	props, _ := params["properties"].(map[string]any)
	ref := ""
	if m := refRe.FindStringSubmatch(text); m != nil {
		ref = m[1]
	}
	for name := range props {
		switch {
		case name == "ref" || name == "reference" || strings.Contains(name, "code"):
			out[name] = ref
		case strings.Contains(name, "iban") || strings.Contains(name, "account"):
			if m := ibanRe.FindString(text); m != "" {
				out[name] = m
			} else {
				out[name] = ref
			}
		case strings.Contains(name, "to") || strings.Contains(name, "recipient") || strings.Contains(name, "email"):
			if m := mailRe.FindString(text); m != "" {
				out[name] = m
			} else {
				out[name] = ref
			}
		case strings.Contains(name, "amount"):
			out[name] = 100
		default:
			out[name] = ref
		}
	}
	if len(out) == 0 && ref != "" {
		out["ref"] = ref
	}
	return out
}

func main() {
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = "127.0.0.1:7500"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var in req
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		// Everything the model can see this turn, system prompt included: a
		// real agent is just as able to act on content that reached it through
		// the system role, which is exactly what the home target does with its
		// entity list.
		var seen strings.Builder
		for _, m := range in.Messages {
			seen.WriteString(m.Content)
			seen.WriteString("\n")
		}
		text := seen.String()
		low := strings.ToLower(text)

		// A tool-less call is the quarantine model (or the classifier): answer
		// with the structured summary it asked for, carrying the content through.
		if len(in.Tools) == 0 {
			reply := text
			if strings.Contains(low, "json") {
				b, _ := json.Marshal(map[string]string{
					"summary": firstLine(text), "visible_text": firstLine(text),
					"content": firstLine(text), "text": firstLine(text),
				})
				reply = string(b)
			}
			if strings.Contains(low, "prompt injection") || strings.Contains(low, "classifier") {
				// the classifier: block only an explicit override attempt
				verdict := "benign"
				if strings.Contains(low, "ignore previous") || strings.Contains(low, "override") {
					verdict = "injection"
				}
				reply = verdict
			}
			write(w, msg{Role: "assistant", Content: reply}, nil)
			return
		}

		// With tools offered: obey an instruction that names one of them.
		for _, t := range in.Tools {
			name := t.Function.Name
			verb := strings.ReplaceAll(name, "_", " ")
			if strings.Contains(low, strings.ToLower(name)) || strings.Contains(low, verb) {
				args := argsFor(t.Function.Parameters, text)
				enc, _ := json.Marshal(args)
				if verbose {
					log.Printf("calling %s %s", name, enc)
				}
				write(w, msg{Role: "assistant"}, []map[string]any{{
					"id": fmt.Sprintf("call_%d", calls.Load()), "type": "function",
					"function": map[string]any{"name": name, "arguments": string(enc)},
				}})
				return
			}
		}
		write(w, msg{Role: "assistant", Content: "Noted. Nothing requires action."}, nil)
	})
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
	})
	log.Println("fake model on", addr, "— deterministic, obeys any tool named in what it perceives")
	log.Fatal(http.ListenAndServe(addr, mux))
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i > 0 {
		return s[:i]
	}
	if len(s) > 400 {
		return s[:400]
	}
	return s
}

func write(w http.ResponseWriter, m msg, calls []map[string]any) {
	out := map[string]any{"role": m.Role, "content": m.Content}
	if calls != nil {
		out["tool_calls"] = calls
	}
	json.NewEncoder(w).Encode(map[string]any{
		"choices": []any{map[string]any{"message": out, "finish_reason": "stop"}},
	})
}
