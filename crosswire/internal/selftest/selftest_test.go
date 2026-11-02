package selftest

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/cdp"
)

func TestSelftestPassesEveryConnector(t *testing.T) {
	_ = cdp.ProbeForTest(t.Setenv) // let the browser row run where Chrome works
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	var out bytes.Buffer
	rows, ok := Run(ctx, &out, 5)
	t.Log("\n" + out.String())
	if !ok {
		t.Fatalf("selftest failed:\n%s", out.String())
	}
	want := []string{"http", "websocket", "tcp", "mqtt 3.1.1", "voice over websocket", "device bridge (led)", "device bridge (ble)", "http + http observer", "observer, no nonce visible", "input + response mapper"}
	have := map[string]bool{}
	for _, r := range rows {
		if !r.Skipped {
			have[r.Connector] = true
		}
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("no passing row for %q", w)
		}
	}
	if !strings.Contains(out.String(), "passed") {
		t.Error("summary line missing")
	}
}
