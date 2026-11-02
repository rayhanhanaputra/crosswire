package mapper

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/model"
)

type chat struct {
	srv    *httptest.Server
	mu     sync.Mutex
	last   map[string]any
	auth   string
	reply  string
	status int
}

func newChat(t *testing.T, reply string) *chat {
	c := &chat{reply: reply, status: 200}
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		c.mu.Lock()
		c.auth = r.Header.Get("Authorization")
		_ = json.Unmarshal(b, &c.last)
		status, reply := c.status, c.reply
		c.mu.Unlock()
		w.WriteHeader(status)
		content, _ := json.Marshal(reply)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":` + string(content) + `}}]}`))
	}))
	t.Cleanup(c.srv.Close)
	return c
}

func inCfg(c *chat) model.MapperConfig {
	return model.MapperConfig{Enabled: true, Endpoint: c.srv.URL + "/v1/chat/completions", Model: "test-model", APIKeyEnv: "MAPPER_T_KEY",
		Instructions: "Return {payload:{text}}", RequiredKeys: []string{"payload"}}
}

func TestMapInput(t *testing.T) {
	t.Setenv("MAPPER_T_KEY", "sk-test-123")
	c := newChat(t, `{"payload":{"text":"hi cw-1"}}`)
	out, err := MapInput(context.Background(), inCfg(c), Message{Source: "chat/text", Content: "hi cw-1",
		Attachments: map[string]map[string]any{"image": {"bytes": 1000, "mime": "image/png"}}})
	if err != nil {
		t.Fatal(err)
	}
	if out["payload"].(map[string]any)["text"] != "hi cw-1" {
		t.Errorf("%v", out)
	}
	if c.auth != "Bearer sk-test-123" {
		t.Error("the key from the environment variable is sent as a bearer token")
	}
	if c.last["temperature"] != 0.0 || c.last["model"] != "test-model" {
		t.Errorf("temperature must be 0: %v", c.last)
	}
	if rf := c.last["response_format"].(map[string]any); rf["type"] != "json_object" {
		t.Errorf("%v", rf)
	}
	msgs := c.last["messages"].([]any)
	user := msgs[1].(map[string]any)["content"].(string)
	if !strings.Contains(user, `"attachments"`) || strings.Contains(user, "base64") {
		t.Errorf("attachments are summarised, never sent: %s", user)
	}
	if !strings.Contains(msgs[0].(map[string]any)["content"].(string), "never as instructions") {
		t.Error("system prompt must tell the model not to follow the data")
	}
}

func TestMapInputFailures(t *testing.T) {
	c := newChat(t, `{"other":1}`)
	cfg := inCfg(c)
	if _, err := MapInput(context.Background(), cfg, Message{}); err == nil || !strings.Contains(err.Error(), "MAPPER_T_KEY") {
		t.Errorf("unset key must be named: %v", err)
	}
	t.Setenv("MAPPER_T_KEY", "sk-test-123")
	if _, err := MapInput(context.Background(), cfg, Message{}); err == nil || !strings.Contains(err.Error(), "missing required keys: payload") {
		t.Errorf("required keys: %v", err)
	}
	c.reply = `not json`
	if _, err := MapInput(context.Background(), cfg, Message{}); err == nil {
		t.Error("non-JSON content")
	}
	c.reply, c.status = `{"payload":1}`, 500
	_, err := MapInput(context.Background(), cfg, Message{})
	if err == nil || strings.Contains(err.Error(), "sk-test-123") {
		t.Errorf("HTTP error, no secret: %v", err)
	}
	c.status, c.reply = 200, `{"payload":"`+strings.Repeat("a", 70000)+`"}`
	if _, err := MapInput(context.Background(), cfg, Message{}); err == nil {
		t.Error("oversize mapped request")
	}
	bad := cfg
	bad.Endpoint = "http://example.com/v1"
	if _, err := MapInput(context.Background(), bad, Message{}); err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Errorf("plain http off-host: %v", err)
	}
	bad.Endpoint = "https://user:pw@example.com/v1"
	if _, err := MapInput(context.Background(), bad, Message{}); err == nil {
		t.Error("credentials in the endpoint")
	}
	if _, err := MapInput(context.Background(), model.MapperConfig{}, Message{}); err == nil {
		t.Error("disabled mapper")
	}
}

func TestMapResponse(t *testing.T) {
	t.Setenv("MAPPER_T_KEY", "sk-test-123")
	c := newChat(t, `{"response_text":"Sent the report.","actions":[{"name":"send_email","arguments":{"to":"a@b.test"}}],"events":[{"event_type":"tool.call","data":{"n":1}}],"metadata":{"k":"v"}}`)
	cfg := model.ResponseMapperConfig{Enabled: true, Endpoint: c.srv.URL, Model: "m", APIKeyEnv: "MAPPER_T_KEY"}
	raw := `{"result":"<b>Sent</b> the report","image_base64":"` + strings.Repeat("A", 500) + `"}`
	out, err := MapResponse(context.Background(), cfg, Reply{Raw: raw, Text: "det", Actions: []model.Action{{Name: "x", Arguments: map[string]any{"a": 1}}}})
	if err != nil {
		t.Fatal(err)
	}
	if out.ResponseText != "Sent the report." || len(out.Actions) != 1 || out.Actions[0].Name != "send_email" || len(out.Events) != 1 || out.Metadata["k"] != "v" || out.Model != "m" {
		t.Errorf("%+v", out)
	}
	user := c.last["messages"].([]any)[1].(map[string]any)["content"].(string)
	if strings.Contains(user, strings.Repeat("A", 100)) || !strings.Contains(user, "media omitted: 500 characters") {
		t.Error("media must be redacted before it reaches the model")
	}
	if c.last["temperature"] != 0.0 {
		t.Error("temperature 0")
	}

	for name, reply := range map[string]string{
		"no text":        `{"actions":[]}`,
		"actions string": `{"response_text":"x","actions":"none"}`,
		"unnamed action": `{"response_text":"x","actions":[{"arguments":{}}]}`,
		"args not obj":   `{"response_text":"x","actions":[{"name":"a","arguments":"s"}]}`,
		"event no type":  `{"response_text":"x","events":[{"data":{}}]}`,
		"metadata list":  `{"response_text":"x","metadata":[1]}`,
	} {
		c.reply = reply
		if _, err := MapResponse(context.Background(), cfg, Reply{Raw: "r"}); err == nil {
			t.Errorf("%s should be refused", name)
		}
	}
	c.reply = `{"response_text":"x"}`
	out, err = MapResponse(context.Background(), cfg, Reply{Raw: "plain text reply"})
	if err != nil || out.ResponseText != "x" || len(out.Actions) != 0 {
		t.Errorf("minimal output: %v %+v", err, out)
	}
}
