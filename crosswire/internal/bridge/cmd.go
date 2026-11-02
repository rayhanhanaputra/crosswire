package bridge

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/guard"
)

// Main runs `crosswire bridge`. The bridge has no authentication, so it only
// listens on a loopback address.
func Main(args []string) error {
	fs := flag.NewFlagSet("bridge", flag.ContinueOnError)
	kind := fs.String("kind", "led", "device kind: led, gpio, ble or nfc")
	device := fs.String("device-id", "arduino-01", "the one device id this bridge accepts")
	resource := fs.String("resource", "", "the one resource this bridge accepts (default pin-N for led and gpio)")
	pin := fs.Int("pin", 9, "LED pin on the rig")
	listen := fs.String("listen", "127.0.0.1:9910", "loopback address to listen on")
	simulate := fs.Bool("simulate", false, "record commands and drive nothing")
	serialPort := fs.String("serial-port", "", "serial port of the Arduino/ESP32 LED rig (kind led or gpio)")
	helper := fs.String("exec", "", "helper command for BLE, NFC or GPIO, run as `HELPER start|stop` with the request JSON on stdin")
	if err := fs.Parse(args); err != nil {
		return err
	}
	host, _, err := net.SplitHostPort(*listen)
	if err != nil || !guard.IsLoopbackHost(host) {
		return fmt.Errorf("the bridge has no authentication: --listen must be a loopback address, got %q", *listen)
	}
	if *pin < 0 || *pin > 99 {
		return fmt.Errorf("--pin must be from 0 to 99")
	}
	res := *resource
	if res == "" {
		res = fmt.Sprintf("pin-%d", *pin)
	}
	cfg := Config{Kind: *kind, DeviceID: *device, Resource: res, Pin: *pin, Simulate: *simulate, SerialPort: *serialPort, Exec: strings.Fields(*helper)}
	srv, be, err := New(cfg)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ready, _ := json.Marshal(map[string]any{"status": "ready", "url": "http://" + ln.Addr().String(), "kind": cfg.Kind, "device_id": cfg.DeviceID, "resource": cfg.Resource, "simulate": cfg.Simulate})
	fmt.Println(string(ready))
	hs := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- hs.Serve(ln) }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	// Switch the stimulus off on the way out, whatever state it was in.
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = be.Stop(sctx, Command{Operation: "stop", DeviceID: cfg.DeviceID, Resource: cfg.Resource})
	return hs.Shutdown(sctx)
}
