package rig

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/rayhanhanaputra/crosswire/internal/signal"
)

// fakeBoard answers like the firmware: it validates the pin and command name.
type fakeBoard struct {
	in       *io.PipeWriter
	out      *io.PipeReader
	commands []string
}

func newFakeBoard(t *testing.T, pin int) (*fakeBoard, io.ReadWriteCloser) {
	hostR, boardW := io.Pipe()
	boardR, hostW := io.Pipe()
	fb := &fakeBoard{}
	go func() {
		rd := bufio.NewReader(boardR)
		for {
			line, err := rd.ReadBytes('\n')
			if err != nil {
				return
			}
			var cmd map[string]any
			_ = json.Unmarshal(line, &cmd)
			fb.commands = append(fb.commands, cmd["command"].(string))
			reply := `{"status":"ok","detail":"accepted"}`
			if int(cmd["pin"].(float64)) != pin {
				reply = `{"status":"error","detail":"pin_not_allowlisted"}`
			} else if cmd["command"] == "stop" {
				reply = `{"status":"ok","detail":"stopped"}`
			}
			_, _ = boardW.Write([]byte(reply + "\n"))
		}
	}()
	return fb, struct {
		io.Reader
		io.Writer
		io.Closer
	}{hostR, hostW, hostW}
}

func TestBlinkAlwaysStops(t *testing.T) {
	board, stream := newFakeBoard(t, 9)
	r := NewFromReadWriter(stream, 9)
	pulses := signal.MorsePulses(signal.MorseEncode("E"))
	play, stop, err := r.Blink(pulses, 50)
	if err != nil {
		t.Fatal(err)
	}
	if play.Detail != "accepted" || stop.Detail != "stopped" {
		t.Fatalf("replies %+v %+v", play, stop)
	}
	if strings.Join(board.commands, ",") != "led_sequence,stop" {
		t.Fatalf("commands %v", board.commands)
	}
}

func TestWrongPinIsRefusedAndStillStopped(t *testing.T) {
	board, stream := newFakeBoard(t, 9)
	r := NewFromReadWriter(stream, 2)
	_, _, err := r.Blink(signal.MorsePulses("."), 50)
	if err == nil || !strings.Contains(err.Error(), "pin_not_allowlisted") {
		t.Fatalf("expected pin refusal, got %v", err)
	}
	if len(board.commands) != 2 || board.commands[1] != "stop" {
		t.Fatalf("stop not sent after refusal: %v", board.commands)
	}
}

func TestPlayReturnsAtTheAcknowledgementAndStopIsSeparate(t *testing.T) {
	board, stream := newFakeBoard(t, 9)
	r := NewFromReadWriter(stream, 9)
	pulses := signal.MorsePulses(signal.MorseEncode("EEEE")) // several seconds at 400 ms units if it blocked
	start := time.Now()
	reply, err := r.Play(pulses, 400, 60000)
	if err != nil || reply.Detail != "accepted" {
		t.Fatalf("%v %+v", err, reply)
	}
	if time.Since(start) > time.Second {
		t.Errorf("Play must not wait for the pulses: %v", time.Since(start))
	}
	if strings.Join(board.commands, ",") != "led_sequence" {
		t.Errorf("only the sequence so far: %v", board.commands)
	}
	if _, err := r.Stop(); err != nil || board.commands[len(board.commands)-1] != "stop" {
		t.Errorf("stop: %v %v", err, board.commands)
	}
	if _, err := r.Play(pulses, 10, 60000); err == nil {
		t.Error("a unit outside the rig's range is refused before anything is sent")
	}
}
