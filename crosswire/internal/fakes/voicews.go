package fakes

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/wsock"
)

// VoiceWS is a realtime voice service: it takes a start event, audio chunk
// events and an end event, then answers with a transcript event, an audio
// event and a done event. The start event's text goes to the agent.
type VoiceWS struct {
	srv *httptest.Server
	mu  sync.Mutex
	// Chunks and AudioBytes describe the last session.
	Chunks     int
	AudioBytes int
	Start      string
	Cancelled  bool
	// Stall makes the service read the end event and then say nothing.
	Stall bool
	// Endless makes the service never send the done event.
	Endless bool
}

// NewVoiceWS starts the fake.
func NewVoiceWS(a *Agent) *VoiceWS {
	f := &VoiceWS{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := wsock.Accept(w, r, 0)
		if err != nil {
			return
		}
		defer c.Close()
		ctx := context.Background()
		_, first, err := c.Read(ctx)
		if err != nil {
			return
		}
		f.mu.Lock()
		f.Start, f.Chunks, f.AudioBytes, f.Cancelled = string(first), 0, 0, false
		f.mu.Unlock()
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			var ev struct {
				Type  string `json:"type"`
				Audio string `json:"audio"`
			}
			_ = json.Unmarshal(data, &ev)
			switch ev.Type {
			case "audio.chunk":
				raw, _ := base64.StdEncoding.DecodeString(ev.Audio)
				f.mu.Lock()
				f.Chunks++
				f.AudioBytes += len(raw)
				f.mu.Unlock()
			case "session.cancel":
				f.mu.Lock()
				f.Cancelled = true
				f.mu.Unlock()
				return
			case "audio.end":
				if f.Stall {
					time.Sleep(3 * time.Second)
					return
				}
				var reply map[string]any
				_ = json.Unmarshal(a.Reply(string(first)), &reply)
				reply["type"] = "transcript"
				b, _ := json.Marshal(reply)
				_ = c.WriteText(ctx, b)
				if f.Endless {
					for i := 0; i < 20; i++ {
						_ = c.WriteText(ctx, []byte(`{"type":"keepalive"}`))
					}
					continue
				}
				audio, _ := json.Marshal(map[string]any{"type": "audio.delta", "audio": base64.StdEncoding.EncodeToString([]byte("RIFFfake-reply"))})
				_ = c.WriteText(ctx, audio)
				_ = c.WriteText(ctx, []byte(`{"type":"session.done"}`))
				return
			}
		}
	}))
	return f
}

// URL is the ws:// address.
func (f *VoiceWS) URL() string { return "ws" + strings.TrimPrefix(f.srv.URL, "http") + "/voice" }

// Close stops the server.
func (f *VoiceWS) Close() { f.srv.Close() }

// Session returns what the last session sent.
func (f *VoiceWS) Session() (start string, chunks, audioBytes int, cancelled bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Start, f.Chunks, f.AudioBytes, f.Cancelled
}
