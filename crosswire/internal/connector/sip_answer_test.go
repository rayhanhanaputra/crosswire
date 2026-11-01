package connector

import (
	"context"
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
