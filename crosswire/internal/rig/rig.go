// Package rig drives the repository's Arduino/ESP32 LED rig over a serial
// port. It speaks the newline-delimited JSON protocol in
// hardware/arduino/aat_led_rig/aat_led_rig.ino and never returns without
// attempting the stop command, so a crashed run cannot leave the LED on.
package rig

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"go.bug.st/serial"

	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/signal"
)

// Reply is the firmware's acknowledgement line.
type Reply struct {
	Status string `json:"status"`
	Detail string `json:"detail"`
}

// Rig is an open serial connection to one LED rig.
type Rig struct {
	port io.ReadWriteCloser
	rd   *bufio.Reader
	Pin  int
}

// Ports lists serial ports the OS exposes, for the UI's device picker.
func Ports() ([]string, error) {
	return serial.GetPortsList()
}

// Open connects at the firmware's fixed 115200 baud.
func Open(path string, pin int) (*Rig, error) {
	port, err := serial.Open(path, &serial.Mode{BaudRate: 115200})
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	_ = port.SetReadTimeout(3 * time.Second)
	// Many boards reset on open; give the bootloader a moment.
	time.Sleep(1500 * time.Millisecond)
	return &Rig{port: port, rd: bufio.NewReader(port), Pin: pin}, nil
}

// NewFromReadWriter wraps any stream, for tests and simulated rigs.
func NewFromReadWriter(rw io.ReadWriteCloser, pin int) *Rig {
	return &Rig{port: rw, rd: bufio.NewReader(rw), Pin: pin}
}

// Blink plays a pulse timeline and blocks until it should have finished, then
// sends stop regardless of outcome. The returned replies are evidence for the
// run record.
func (r *Rig) Blink(pulses []signal.Pulse, unitMs int) (play Reply, stop Reply, err error) {
	duration := signal.PulsesDuration(pulses, unitMs)
	cmd, err := signal.LEDSequenceCommand(pulses, r.Pin, unitMs, duration+2000, 256)
	if err != nil {
		return play, stop, err
	}
	play, err = r.send(cmd)
	if err == nil && play.Status != "ok" {
		err = fmt.Errorf("rig refused sequence: %s", play.Detail)
	}
	if err == nil {
		time.Sleep(time.Duration(duration+200) * time.Millisecond)
	}
	stop, stopErr := r.Stop()
	if err == nil {
		err = stopErr
	}
	return play, stop, err
}

// Play starts a pulse timeline and returns when the firmware has acknowledged
// it. The firmware plays it on its own and its watchdog switches the LED off
// after watchdogMs; the caller must still call Stop.
func (r *Rig) Play(pulses []signal.Pulse, unitMs, watchdogMs int) (Reply, error) {
	cmd, err := signal.LEDSequenceCommand(pulses, r.Pin, unitMs, watchdogMs, 256)
	if err != nil {
		return Reply{}, err
	}
	reply, err := r.send(cmd)
	if err == nil && reply.Status != "ok" {
		err = fmt.Errorf("rig refused sequence: %s", reply.Detail)
	}
	return reply, err
}

// Stop sends the mandatory stop command.
func (r *Rig) Stop() (Reply, error) {
	reply, err := r.send(signal.LEDStopCommand(r.Pin))
	if err != nil {
		return reply, fmt.Errorf("emergency stop failed: %w", err)
	}
	if reply.Status != "ok" {
		return reply, fmt.Errorf("rig refused stop: %s", reply.Detail)
	}
	return reply, nil
}

// Close stops the rig and closes the port.
func (r *Rig) Close() error {
	_, stopErr := r.Stop()
	closeErr := r.port.Close()
	return errors.Join(stopErr, closeErr)
}

func (r *Rig) send(cmd map[string]any) (Reply, error) {
	line, err := json.Marshal(cmd)
	if err != nil {
		return Reply{}, err
	}
	if _, err := r.port.Write(append(line, '\n')); err != nil {
		return Reply{}, fmt.Errorf("write: %w", err)
	}
	raw, err := r.rd.ReadBytes('\n')
	if err != nil {
		return Reply{}, fmt.Errorf("no reply from rig: %w", err)
	}
	var reply Reply
	if err := json.Unmarshal(raw, &reply); err != nil {
		return Reply{}, fmt.Errorf("rig sent non-JSON %q", string(raw))
	}
	return reply, nil
}
