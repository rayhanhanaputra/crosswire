package observer

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/cdp"
	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/fakes"
	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/model"
)

const canary = "cw-0a1b2c3d"

func TestStateText(t *testing.T) {
	tests := []struct{ body, path, want string }{
		{`{"door":{"state":"open"},"n":2}`, "door.state", "open"},
		{`{"door":{"state":"open"}}`, "", `{"door":{"state":"open"}}`},
		{`{"list":["a","b"]}`, "list.1", "b"},
		{`{"list":["a"]}`, "list.5", ""},
		{`{"a":1}`, "a.b", ""},
		{"  plain text \n", "", "plain text"},
		{`{"n":3}`, "n", "3"},
	}
	for _, tc := range tests {
		if got := stateText(tc.body, tc.path); got != tc.want {
			t.Errorf("stateText(%q,%q) = %q, want %q", tc.body, tc.path, got, tc.want)
		}
	}
}

func TestCompareAndFindings(t *testing.T) {
	obs := func(text string) *model.Observation { return &model.Observation{Text: text} }
	tests := []struct {
		name          string
		before, after string
		nonce         string
		changed       bool
		carries       bool
		rule          string
		hint          bool
	}{
		{"no change", "locked", "locked", "", false, false, "", false},
		{"whitespace only", "locked  now", "locked\nnow", "", false, false, "", false},
		{"nonce visible", "locked", "UNLOCKED " + canary, "", true, true, "observer_change", false},
		{"nonce visible with ocr noise", "locked", "UNLOCKED CW 0A1B 2C3D", "", true, true, "observer_change", false},
		{"changed, nonce required, not shown", "locked", "UNLOCKED", "", true, false, "observer_change", true},
		{"changed, nonce not visible target", "locked", "UNLOCKED", "not_visible", true, false, "observer_change_unattributed", false},
		{"nonce already there before", canary + " locked", canary + " UNLOCKED", "", true, false, "observer_change", true},
	}
	for _, tc := range tests {
		rec := Compare(obs(tc.before), obs(tc.after), canary)
		if rec.Changed != tc.changed || rec.Carries != tc.carries {
			t.Errorf("%s: changed=%v carries=%v", tc.name, rec.Changed, rec.Carries)
		}
		f := Findings(model.ObserverConfig{Nonce: tc.nonce}, rec)
		if tc.rule == "" {
			if len(f) != 0 {
				t.Errorf("%s: unexpected findings %+v", tc.name, f)
			}
			continue
		}
		if len(f) != 1 || f[0].Rule != tc.rule || f[0].Hint != tc.hint {
			t.Errorf("%s: findings %+v", tc.name, f)
		}
	}
	if rec := Compare(nil, obs("x"), canary); rec.Changed || rec.Detail == "" {
		t.Error("a missing read cannot be judged")
	}
	if rec := Compare(&model.Observation{Error: "boom"}, obs("x"), canary); rec.Changed {
		t.Error("a failed read cannot be judged")
	}
}

func TestHTTPObserver(t *testing.T) {
	page := fakes.NewStatePage(&fakes.Agent{Name: "p"}, fakes.StatePageOptions{ShowNonce: true})
	defer page.Close()
	t.Setenv("OBS_T_TOKEN", "tok")
	o := New(model.ObserverConfig{Kind: "http", URL: page.StateURL(), Path: "door.state", SecretHeaders: map[string]string{"X-Key": "OBS_T_TOKEN"}})
	defer o.Close()
	if err := o.Preflight(); err != nil {
		t.Fatal(err)
	}
	before, err := o.Read(context.Background(), "before")
	if err != nil || before.Obs.Text != "locked" || before.Obs.Status != 200 || before.Obs.Phase != "before" || before.Obs.Kind != "http" {
		t.Fatalf("%v %+v", err, before.Obs)
	}
	page.Set("UNLOCKED " + canary)
	after, _ := o.Read(context.Background(), "after")
	rec := Compare(&before.Obs, &after.Obs, canary)
	if !rec.Changed || !rec.Carries {
		t.Errorf("%+v", rec)
	}
}

func TestHTTPObserverFailures(t *testing.T) {
	o := New(model.ObserverConfig{Kind: "http", URL: "http://127.0.0.1:1/state", TimeoutSeconds: 2})
	if _, err := o.Read(context.Background(), "before"); err == nil {
		t.Error("connection refused")
	}
	o = New(model.ObserverConfig{Kind: "http", URL: "http://169.254.169.254/latest", TimeoutSeconds: 2})
	if _, err := o.Read(context.Background(), "before"); err == nil || !strings.Contains(err.Error(), "link-local") {
		t.Errorf("metadata address: %v", err)
	}
	o = New(model.ObserverConfig{Kind: "http", URL: "http://127.0.0.1:1/", SecretHeaders: map[string]string{"X": "OBS_T_UNSET_VAR"}})
	if err := o.Preflight(); err == nil || !strings.Contains(err.Error(), "OBS_T_UNSET_VAR") {
		t.Errorf("preflight: %v", err)
	}
	if New(model.ObserverConfig{}) != nil || New(model.ObserverConfig{Kind: "none"}) != nil {
		t.Error("disabled observer must be nil")
	}
	var nilObs *Observer
	nilObs.Close()
}

func TestHTTPObserverStatusAndSize(t *testing.T) {
	page := fakes.NewStatePage(&fakes.Agent{}, fakes.StatePageOptions{})
	defer page.Close()
	o := New(model.ObserverConfig{Kind: "http", URL: strings.TrimSuffix(page.URL(), "/") + "/missing"})
	r, err := o.Read(context.Background(), "before")
	if err == nil || r.Obs.Status != 404 || r.Obs.Error == "" {
		t.Errorf("404 must be an error with the status kept: %v %+v", err, r.Obs)
	}
}

func chromeOrSkip(t *testing.T) {
	t.Helper()
	if err := cdp.ProbeForTest(t.Setenv); err != nil {
		t.Skipf("no usable Chrome or Chromium: %v", err)
	}
}

func TestBrowserObserverReadsTextAndScreenshot(t *testing.T) {
	chromeOrSkip(t)
	page := fakes.NewStatePage(&fakes.Agent{Name: "p"}, fakes.StatePageOptions{ShowNonce: true})
	defer page.Close()
	o := New(model.ObserverConfig{Kind: "browser", URL: page.URL(), Selector: "#status", Screenshot: true, TimeoutSeconds: 20})
	defer o.Close()
	before, err := o.Read(context.Background(), "before")
	if err != nil {
		t.Fatal(err)
	}
	if before.Obs.Text != "locked" || len(before.Screenshot) < 500 || string(before.Screenshot[1:4]) != "PNG" {
		t.Fatalf("before: %q, %d screenshot bytes", before.Obs.Text, len(before.Screenshot))
	}
	page.Set("UNLOCKED " + canary)
	after, err := o.Read(context.Background(), "after")
	if err != nil || after.Obs.Text != "UNLOCKED "+canary {
		t.Fatalf("after: %v %q", err, after.Obs.Text)
	}
	if rec := Compare(&before.Obs, &after.Obs, canary); !rec.Changed || !rec.Carries {
		t.Errorf("%+v", rec)
	}
}

func TestBrowserObserverLivePageSeesPushedUpdate(t *testing.T) {
	chromeOrSkip(t)
	page := fakes.NewStatePage(&fakes.Agent{}, fakes.StatePageOptions{})
	defer page.Close()
	o := New(model.ObserverConfig{Kind: "browser", URL: page.URL(), Selector: "#status", Live: true, TimeoutSeconds: 20})
	defer o.Close()
	before, err := o.Read(context.Background(), "before")
	if err != nil || before.Obs.Text != "locked" {
		t.Fatalf("%v %q", err, before.Obs.Text)
	}
	page.Set("pushed")
	time.Sleep(600 * time.Millisecond)
	after, err := o.Read(context.Background(), "after")
	if err != nil || after.Obs.Text != "pushed" {
		t.Fatalf("a live page picks up the script's update without a reload: %v %q", err, after.Obs.Text)
	}
}

func TestBrowserObserverMissingSelectorAndCrossOrigin(t *testing.T) {
	chromeOrSkip(t)
	page := fakes.NewStatePage(&fakes.Agent{}, fakes.StatePageOptions{})
	defer page.Close()
	o := New(model.ObserverConfig{Kind: "browser", URL: page.URL(), Selector: "#nope", TimeoutSeconds: 2})
	defer o.Close()
	if _, err := o.Read(context.Background(), "before"); err == nil || !strings.Contains(err.Error(), "#nope") {
		t.Errorf("missing selector: %v", err)
	}
}

func TestBrowserObserverWithoutChromeSaysSo(t *testing.T) {
	t.Setenv("CROSSWIRE_CHROME", "/nonexistent/chrome")
	o := New(model.ObserverConfig{Kind: "browser", URL: "http://127.0.0.1:1/", Selector: "#x"})
	err := o.Preflight()
	if err == nil || !strings.Contains(err.Error(), "CROSSWIRE_CHROME") {
		t.Errorf("preflight: %v", err)
	}
	if _, err := o.Read(context.Background(), "before"); err == nil {
		t.Error("read without a browser must fail")
	}
	os.Unsetenv("CROSSWIRE_CHROME")
}
