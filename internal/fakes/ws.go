package fakes

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"

	"github.com/rayhanhanaputra/crosswire/internal/wsock"
)

// WS is a WebSocket agent: it reads one text message and answers with the
// agent's reply.
type WS struct {
	srv *httptest.Server
	mu  sync.Mutex
	hdr http.Header
	sub string
	got []string
}

// NewWS starts a fake WebSocket agent on a loopback port.
func NewWS(a *Agent) *WS { return newWS(a, nil) }

// NewWSTLS starts the fake behind TLS (wss://).
func NewWSTLS(a *Agent, cfg *tls.Config) *WS { return newWS(a, cfg) }

func newWS(a *Agent, cfg *tls.Config) *WS {
	f := &WS{}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.hdr = r.Header.Clone()
		f.sub = r.Header.Get("Sec-WebSocket-Protocol")
		f.mu.Unlock()
		c, err := wsock.Accept(w, r, 0)
		if err != nil {
			return
		}
		defer c.Close()
		_, data, err := c.Read(context.Background())
		if err != nil {
			return
		}
		f.mu.Lock()
		f.got = append(f.got, string(data))
		f.mu.Unlock()
		_ = c.WriteText(context.Background(), a.Reply(string(data)))
	})
	if cfg == nil {
		f.srv = httptest.NewServer(h)
	} else {
		f.srv = httptest.NewUnstartedServer(h)
		f.srv.TLS = cfg
		f.srv.StartTLS()
	}
	return f
}

// URL is the ws:// address.
func (f *WS) URL() string { return "ws" + strings.TrimPrefix(f.srv.URL, "http") + "/agent" }

// Close stops the server.
func (f *WS) Close() { f.srv.Close() }

// Header returns the headers of the last upgrade request.
func (f *WS) Header() http.Header {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hdr
}

// Subprotocol is what the last client asked for.
func (f *WS) Subprotocol() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sub
}

// Received lists the messages the agent has read.
func (f *WS) Received() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.got...)
}
