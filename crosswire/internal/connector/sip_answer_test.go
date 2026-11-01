package connector

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/model"
	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/signal"
)

func TestAnswererRecordsCallerPlaysReplyAndHangsUp(t *testing.T) {
	got := make(chan []byte, 1)
	a, err := NewAnswerer("127.0.0.1:0", func(ctx context.Context, wav []byte) ([]byte, error) {
		got <- wav
		return signal.WAV(8000, signal.Tone(8000, 4000, 0.5, 600)), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	a.Idle = 400 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	go a.Serve(ctx)
	call, err := Dial(ctx, model.SIPConfig{LocalIP: "127.0.0.1", ListenSeconds: 10}, fmt.Sprintf("sip:x@127.0.0.1:%d", a.Addr().Port), testWAV())
	if err != nil {
		t.Fatalf("%v\n%s", err, strings.Join(call.Log, "\n"))
	}
	if !call.RemoteHungUp || call.ReceivedSeconds < 0.4 || len(call.ReplyAudio) == 0 {
		t.Fatalf("reply not played or no BYE: %+v", call)
	}
	select {
	case wav := <-got:
		rate, samples, err := signal.ParseWAV(wav)
		if err != nil || rate != 8000 || len(samples) < 7000 {
			t.Fatalf("handler audio: rate %d samples %d err %v", rate, len(samples), err)
		}
	default:
		t.Fatal("handler never ran")
	}
}

func TestAnswererRefusesNonPCMUOffer(t *testing.T) {
	a, err := NewAnswerer("127.0.0.1:0", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.Serve(ctx)
	c, err := net.DialUDP("udp", nil, a.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	sdp := "v=0\r\nc=IN IP4 127.0.0.1\r\nm=audio 4000 RTP/AVP 8\r\n"
	inv := fmt.Sprintf("INVITE sip:x@127.0.0.1 SIP/2.0\r\nVia: SIP/2.0/UDP 127.0.0.1:5999;branch=z9hG4bKx\r\nFrom: <sip:a@127.0.0.1>;tag=1\r\nTo: <sip:x@127.0.0.1>\r\nCall-ID: c1\r\nCSeq: 1 INVITE\r\nContent-Type: application/sdp\r\nContent-Length: %d\r\n\r\n%s", len(sdp), sdp)
	_, _ = c.Write([]byte(inv))
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 2048)
	n, err := c.Read(buf)
	if err != nil || !strings.HasPrefix(string(buf[:n]), "SIP/2.0 488") {
		t.Fatalf("want 488, got %q err %v", buf[:n], err)
	}
}

func TestAnswererHangsUpOnSilentCaller(t *testing.T) {
	a, err := NewAnswerer("127.0.0.1:0", func(context.Context, []byte) ([]byte, error) {
		t.Error("handler must not run without audio")
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	a.NoMedia = 500 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	go a.Serve(ctx)
	// An empty WAV sends no RTP at all.
	call, err := Dial(ctx, model.SIPConfig{LocalIP: "127.0.0.1", ListenSeconds: 8}, fmt.Sprintf("sip:x@127.0.0.1:%d", a.Addr().Port), signal.WAV(8000, nil))
	if err != nil {
		t.Fatalf("%v\n%s", err, strings.Join(call.Log, "\n"))
	}
	if !call.RemoteHungUp {
		t.Fatalf("a call with no media must be ended by the listener: %+v", call)
	}
}

func TestAnswererPassesTheInviteSessionToTheHandler(t *testing.T) {
	seen := make(chan string, 2)
	a, err := NewAnswerer("127.0.0.1:0", func(ctx context.Context, wav []byte) ([]byte, error) {
		seen <- SessionFrom(ctx)
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	a.Idle = 400 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	go a.Serve(ctx)
	uri := fmt.Sprintf("sip:x@127.0.0.1:%d", a.Addr().Port)
	cfg := model.SIPConfig{LocalIP: "127.0.0.1", ListenSeconds: 10}
	for _, want := range []string{"cw-1a2b3c4d", ""} {
		if _, err := DialSession(ctx, cfg, uri, testWAV(), want); err != nil {
			t.Fatal(err)
		}
		if got := <-seen; got != want {
			t.Fatalf("handler saw session %q, want %q", got, want)
		}
	}
}

func TestHandlerFailureIsSentOnTheByeAndFailsTheExchange(t *testing.T) {
	a, err := NewAnswerer("127.0.0.1:0", func(ctx context.Context, wav []byte) ([]byte, error) {
		return nil, fmt.Errorf("provider quota\r\nX-Injected: yes")
	})
	if err != nil {
		t.Fatal(err)
	}
	a.Idle = 400 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	go a.Serve(ctx)
	uri := fmt.Sprintf("sip:x@127.0.0.1:%d", a.Addr().Port)
	call, err := DialSession(ctx, model.SIPConfig{LocalIP: "127.0.0.1", ListenSeconds: 10}, uri, testWAV(), "cw-1")
	if err != nil || !strings.HasPrefix(call.Error, "provider quota") || strings.Contains(call.Error, "\n") {
		t.Fatalf("call error %q err %v", call.Error, err)
	}
	tgt := model.Target{Transport: "sip", Endpoint: uri, SIP: model.SIPConfig{LocalIP: "127.0.0.1", ListenSeconds: 10}}
	ex, err := Send(ctx, tgt, Perception, Input{AudioBase64: testWAVBase64(), Canary: "cw-2"})
	if err == nil || !strings.HasPrefix(ex.Error, "target error: provider quota") {
		t.Fatalf("exchange error %q err %v", ex.Error, err)
	}
	if !strings.HasPrefix(ex.RawResponse, `{"error"`) {
		t.Fatalf("raw response %q", ex.RawResponse)
	}
}

func testWAVBase64() string { return base64.StdEncoding.EncodeToString(testWAV()) }
