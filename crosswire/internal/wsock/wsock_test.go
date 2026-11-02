package wsock

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func echoServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := Accept(w, r, 0)
		if err != nil {
			return
		}
		defer c.Close()
		for {
			typ, data, err := c.Read(context.Background())
			if err != nil {
				return
			}
			if string(data) == "ping-me" {
				c.wmu.Lock()
				_ = c.writeFrame(true, opPing, []byte("x"))
				c.wmu.Unlock()
			}
			if typ == Text {
				_ = c.WriteText(context.Background(), append([]byte("echo:"), data...))
			} else {
				_ = c.WriteBinary(context.Background(), data)
			}
		}
	}))
}

func TestRoundTrip(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/x?y=1", http.Header{"X-Test": {"1"}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, size := range []int{0, 5, 125, 126, 70000} {
		msg := strings.Repeat("a", size)
		if err := c.WriteText(ctx, []byte(msg)); err != nil {
			t.Fatal(err)
		}
		typ, data, err := c.Read(ctx)
		if err != nil || typ != Text || string(data) != "echo:"+msg {
			t.Fatalf("size %d: typ=%d err=%v len=%d", size, typ, err, len(data))
		}
	}
	if err := c.WriteBinary(ctx, []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	if typ, data, err := c.Read(ctx); err != nil || typ != Binary || len(data) != 3 {
		t.Fatalf("binary: %d %v %v", typ, data, err)
	}
	// A ping from the server is answered and skipped.
	_ = c.WriteText(ctx, []byte("ping-me"))
	if _, data, err := c.Read(ctx); err != nil || string(data) != "echo:ping-me" {
		t.Fatalf("after ping: %q %v", data, err)
	}
}

func TestMaxMessageAndTimeout(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil, Options{MaxMessage: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.WriteText(ctx, []byte(strings.Repeat("b", 500)))
	if _, _, err := c.Read(ctx); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("want size error, got %v", err)
	}
	c2, err := Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	short, cancel2 := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel2()
	if _, _, err := c2.Read(short); err != context.DeadlineExceeded {
		t.Fatalf("want deadline, got %v", err)
	}
}

func TestDialRefusals(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := Dial(ctx, "http://x", nil, Options{}); err == nil {
		t.Error("http scheme must be refused")
	}
	if _, err := Dial(ctx, "ws://169.254.169.254/", nil, Options{}); err == nil {
		t.Error("link-local destination must be refused")
	}
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer plain.Close()
	if _, err := Dial(ctx, "ws"+strings.TrimPrefix(plain.URL, "http"), nil, Options{}); err == nil {
		t.Error("a server that does not upgrade must be an error")
	}
}
