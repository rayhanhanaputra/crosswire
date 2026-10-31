// Package connector delivers an encoded payload to a target and captures the
// reply. Only HTTP is implemented; it is what every real agent gateway speaks
// and what the reference targets expose.
package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/model"
)

const maxResponseBytes = 2 << 20

// Input is what the connector has to deliver.
type Input struct {
	Source      string
	Content     string
	ImageBase64 string
	AudioBase64 string
	Canary      string
}

// Arm selects which of the target's two paths to use.
type Arm string

const (
	// Perception is the physical channel (target.Endpoint).
	Perception Arm = "perception"
	// Direct is the chat path (target.DirectEndpoint).
	Direct Arm = "direct"
)

// Render fills the target's request template. Placeholders are replaced with
// JSON-safe strings so a payload containing quotes cannot break the request.
func Render(t model.Target, in Input) (map[string]any, error) {
	return render(t.RequestTemplate, `{"source":"{{source}}","input":"{{content}}"}`, in)
}

// RenderDirect fills the chat-path template.
func RenderDirect(t model.Target, in Input) (map[string]any, error) {
	return render(t.DirectTemplate, `{"message":"{{content}}"}`, in)
}

func render(tpl, def string, in Input) (map[string]any, error) {
	if strings.TrimSpace(tpl) == "" {
		tpl = def
	}
	q := func(s string) string {
		enc, _ := json.Marshal(s)
		return string(enc[1 : len(enc)-1])
	}
	r := strings.NewReplacer(
		"{{content}}", q(in.Content),
		"{{content_json}}", jsonOrQuoted(in.Content),
		"{{source}}", q(in.Source),
		"{{image_base64}}", in.ImageBase64,
		"{{audio_base64}}", in.AudioBase64,
		"{{canary}}", q(in.Canary),
	)
	var body map[string]any
	if err := json.Unmarshal([]byte(r.Replace(tpl)), &body); err != nil {
		return nil, fmt.Errorf("request template is not valid JSON after substitution: %w", err)
	}
	return body, nil
}

func jsonOrQuoted(s string) string {
	if json.Valid([]byte(s)) {
		return s
	}
	enc, _ := json.Marshal(s)
	return string(enc)
}

// Send performs one HTTP exchange on the chosen arm. It refuses redirects.
// The loopback-only rule applies to Crosswire's own API, not to targets,
// which the operator configured explicitly.
func Send(ctx context.Context, t model.Target, arm Arm, in Input) (model.Exchange, error) {
	var ex model.Exchange
	endpoint := t.Endpoint
	var body map[string]any
	var err error
	if arm == Direct {
		endpoint = t.DirectEndpoint
		if endpoint == "" {
			return ex, errors.New("target has no direct (chat) endpoint")
		}
		body, err = RenderDirect(t, in)
	} else {
		body, err = Render(t, in)
	}
	if err != nil {
		return ex, err
	}
	ex.Request = body
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ex, errors.New("target endpoint must be an absolute http(s) URL")
	}
	enc, _ := json.Marshal(body)
	method := t.Method
	if method == "" {
		method = http.MethodPost
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(enc))
	if err != nil {
		return ex, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range t.Headers {
		req.Header.Set(k, v)
	}
	for k, envName := range t.SecretHeaders {
		v := os.Getenv(envName)
		if v == "" {
			return ex, fmt.Errorf("secret header %s: environment variable %s is not set", k, envName)
		}
		req.Header.Set(k, v)
	}
	timeout := time.Duration(t.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("redirects are not followed")
		},
		Transport: &http.Transport{
			DialContext:         (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
			TLSHandshakeTimeout: 10 * time.Second,
		},
	}
	start := time.Now()
	resp, err := client.Do(req)
	ex.DurationMs = int(time.Since(start).Milliseconds())
	if err != nil {
		ex.Error = err.Error()
		return ex, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if len(raw) > maxResponseBytes {
		ex.Error = "response exceeds 2 MiB"
		return ex, errors.New(ex.Error)
	}
	ex.Status = resp.StatusCode
	ex.RawResponse = string(raw)
	ex.ResponseText, ex.Actions, ex.Perceived = Parse(ex.RawResponse, t.ResponsePath, t.ActionsPath, t.PerceivedPath)
	return ex, nil
}

// Parse extracts the reply text, the action list and the perceived text
// from a response body using dot paths ("result.text", "actions",
// "choices.0.message.content"). Empty paths use the contract defaults
// (text / actions / perceived). A non-JSON body is returned whole as the text.
func Parse(raw, responsePath, actionsPath, perceivedPath string) (string, []model.Action, string) {
	var doc any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return raw, nil, ""
	}
	if responsePath == "" {
		responsePath = "text"
	}
	if actionsPath == "" {
		actionsPath = "actions"
	}
	if perceivedPath == "" {
		perceivedPath = "perceived"
	}
	text := raw
	if v, ok := lookup(doc, responsePath); ok {
		text = asString(v)
	}
	perceived := ""
	if v, ok := lookup(doc, perceivedPath); ok {
		perceived = asString(v)
	}
	var actions []model.Action
	if actionsPath != "" {
		if v, ok := lookup(doc, actionsPath); ok {
			if list, isList := v.([]any); isList {
				for _, item := range list {
					if m, isMap := item.(map[string]any); isMap {
						a := model.Action{Arguments: map[string]any{}}
						if n, ok := m["name"].(string); ok {
							a.Name = n
						} else if n, ok := m["tool"].(string); ok {
							a.Name = n
						}
						if args, ok := m["arguments"].(map[string]any); ok {
							a.Arguments = args
						} else if args, ok := m["args"].(map[string]any); ok {
							a.Arguments = args
						}
						if a.Name != "" {
							actions = append(actions, a)
						}
					}
				}
			}
		}
	}
	return text, actions, perceived
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	enc, _ := json.Marshal(v)
	return string(enc)
}

func lookup(doc any, path string) (any, bool) {
	cur := doc
	for _, part := range strings.Split(path, ".") {
		switch node := cur.(type) {
		case map[string]any:
			v, ok := node[part]
			if !ok {
				return nil, false
			}
			cur = v
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(node) {
				return nil, false
			}
			cur = node[i]
		default:
			return nil, false
		}
	}
	return cur, true
}
