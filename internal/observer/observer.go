// Package observer reads a target's state out of band, before and after a
// trial: an HTTP state endpoint, or a web page in a headless browser. The
// engine turns a change between the two reads into evidence under the rules in
// docs/CONTRACT.md section 8: it must carry the trial's nonce where the page
// can show one, it is checked on the control arm too, and a change on the
// control arm voids the run.
package observer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rayhanhanaputra/crosswire/internal/cdp"
	"github.com/rayhanhanaputra/crosswire/internal/evidence"
	"github.com/rayhanhanaputra/crosswire/internal/guard"
	"github.com/rayhanhanaputra/crosswire/internal/model"
)

const (
	maxBody       = 1 << 20
	maxScreenshot = 5 << 20
	maxText       = 1 << 20
	defaultWaitMs = 1000
)

// Reading is one observation and, for a browser, its screenshot.
type Reading struct {
	Obs        model.Observation
	Screenshot []byte
}

// Observer reads the state of one target. Close releases the browser, if any.
type Observer struct {
	cfg  model.ObserverConfig
	b    *cdp.Browser
	page *cdp.Page
}

// Enabled reports whether the config asks for an observer.
func Enabled(cfg model.ObserverConfig) bool { return cfg.Kind == "http" || cfg.Kind == "browser" }

// New returns an observer for the config, or nil when it is disabled.
func New(cfg model.ObserverConfig) *Observer {
	if !Enabled(cfg) {
		return nil
	}
	return &Observer{cfg: cfg}
}

// Enabled reports whether the observer is live (it is not nil).
func (o *Observer) Enabled() bool { return o != nil }

// Wait is how long to wait after a trial before the second read.
func (o *Observer) Wait() time.Duration {
	if o.cfg.WaitMs <= 0 {
		return defaultWaitMs * time.Millisecond
	}
	return time.Duration(o.cfg.WaitMs) * time.Millisecond
}

// Preflight reports a problem that would make every read fail, before the run
// sends anything: a missing browser, an unset secret header variable.
func (o *Observer) Preflight() error {
	switch o.cfg.Kind {
	case "browser":
		if _, err := cdp.FindChrome(); err != nil {
			return fmt.Errorf("browser observer: %w", err)
		}
	case "http":
		for h, env := range o.cfg.SecretHeaders {
			if os.Getenv(env) == "" {
				return fmt.Errorf("http observer: header %s: environment variable %s is not set", h, env)
			}
		}
	}
	return nil
}

func (o *Observer) timeout() time.Duration {
	if o.cfg.TimeoutSeconds <= 0 {
		return 10 * time.Second
	}
	return time.Duration(o.cfg.TimeoutSeconds) * time.Second
}

// Reset asks the target to put its observed state back, with a POST to the
// configured reset URL. It does nothing when no reset URL is set.
func (o *Observer) Reset(ctx context.Context) error {
	if o.cfg.ResetURL == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, o.timeout())
	defer cancel()
	u, err := guard.ParseURL(o.cfg.ResetURL, "http", "https")
	if err != nil {
		return fmt.Errorf("observer reset: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader("{}"))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for h, env := range o.cfg.SecretHeaders {
		if v := os.Getenv(env); v != "" {
			req.Header.Set(h, v)
		}
	}
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirects are not followed") },
		Transport:     &http.Transport{DialContext: guard.DialContext(true, 10*time.Second), TLSHandshakeTimeout: 10 * time.Second},
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("observer reset: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBody))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("observer reset returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// Read takes one observation. phase is "before" or "after".
func (o *Observer) Read(ctx context.Context, phase string) (Reading, error) {
	ctx, cancel := context.WithTimeout(ctx, o.timeout())
	defer cancel()
	var r Reading
	var err error
	switch o.cfg.Kind {
	case "http":
		r, err = o.readHTTP(ctx)
	case "browser":
		r, err = o.readBrowser(ctx, phase)
	default:
		err = errors.New("observer is not configured")
	}
	r.Obs.Kind, r.Obs.Phase, r.Obs.Source = o.cfg.Kind, phase, o.cfg.URL
	if err != nil {
		r.Obs.Error = err.Error()
	}
	return r, err
}

func (o *Observer) readHTTP(ctx context.Context) (Reading, error) {
	var r Reading
	u, err := guard.ParseURL(o.cfg.URL, "http", "https")
	if err != nil {
		return r, fmt.Errorf("http observer: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return r, err
	}
	req.Header.Set("Accept", "application/json, text/plain")
	for h, env := range o.cfg.SecretHeaders {
		v := os.Getenv(env)
		if v == "" {
			return r, fmt.Errorf("http observer: header %s: environment variable %s is not set", h, env)
		}
		req.Header.Set(h, v)
	}
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirects are not followed") },
		Transport:     &http.Transport{DialContext: guard.DialContext(true, 10*time.Second), TLSHandshakeTimeout: 10 * time.Second},
	}
	resp, err := client.Do(req)
	if err != nil {
		return r, fmt.Errorf("http observer: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if len(body) > maxBody {
		return r, errors.New("http observer: state exceeds 1 MiB")
	}
	r.Obs.Status = resp.StatusCode
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return r, fmt.Errorf("http observer: state endpoint returned HTTP %d", resp.StatusCode)
	}
	r.Obs.Text = stateText(string(body), o.cfg.Path)
	return r, nil
}

// stateText reduces a state response to the text that is compared. With a
// path it is the JSON value there; without one, the compact JSON or the text.
func stateText(body, path string) string {
	var doc any
	if json.Unmarshal([]byte(body), &doc) != nil {
		return strings.TrimSpace(body)
	}
	if path != "" {
		cur := doc
		for _, part := range strings.Split(path, ".") {
			switch n := cur.(type) {
			case map[string]any:
				cur = n[part]
			case []any:
				i, err := strconv.Atoi(part)
				if err != nil || i < 0 || i >= len(n) {
					return ""
				}
				cur = n[i]
			default:
				return ""
			}
		}
		doc = cur
	}
	if s, ok := doc.(string); ok {
		return s
	}
	b, _ := json.Marshal(doc)
	return string(b)
}

func (o *Observer) readBrowser(ctx context.Context, phase string) (Reading, error) {
	var r Reading
	if o.cfg.URL == "" || o.cfg.Selector == "" {
		return r, errors.New("browser observer needs a URL and a CSS selector")
	}
	u, err := guard.ParseURL(o.cfg.URL, "http", "https")
	if err != nil {
		return r, fmt.Errorf("browser observer: %w", err)
	}
	if _, err := guard.Resolve(ctx, u.Hostname(), true); err != nil {
		return r, fmt.Errorf("browser observer: %w", err)
	}
	if o.b == nil {
		path, err := cdp.FindChrome()
		if err != nil {
			return r, err
		}
		b, err := cdp.Launch(ctx, path)
		if err != nil {
			return r, fmt.Errorf("browser observer: %w", err)
		}
		o.b = b
	}
	page := o.page
	fresh := page == nil
	if fresh || !o.cfg.Live {
		if page != nil {
			page.Close()
			o.page = nil
		}
		page, err = o.b.NewPage(ctx, cdp.Origin(o.cfg.URL))
		if err != nil {
			return r, fmt.Errorf("browser observer: %w", err)
		}
		if err := page.Navigate(ctx, o.cfg.URL); err != nil {
			page.Close()
			return r, fmt.Errorf("browser observer: %w", err)
		}
		if o.cfg.Live {
			o.page = page
		}
	}
	if !o.cfg.Live {
		defer page.Close()
	}
	if loc, err := page.Location(ctx); err == nil && !sameOriginURL(loc, o.cfg.URL) {
		return r, errors.New("browser observer: navigation changed origin")
	}
	text, err := page.VisibleText(ctx, o.cfg.Selector)
	if err != nil {
		return r, fmt.Errorf("browser observer: %w", err)
	}
	if len(text) > maxText {
		text = text[:maxText]
	}
	r.Obs.Text = text
	if o.cfg.Screenshot {
		png, err := page.Screenshot(ctx, maxScreenshot)
		if err != nil {
			return r, fmt.Errorf("browser observer: %w", err)
		}
		r.Screenshot = png
	}
	return r, nil
}

func sameOriginURL(a, b string) bool {
	ua, err1 := url.Parse(a)
	ub, err2 := url.Parse(b)
	return err1 == nil && err2 == nil && ua.Scheme == ub.Scheme && ua.Host == ub.Host
}

// Close shuts the browser down.
func (o *Observer) Close() {
	if o == nil {
		return
	}
	if o.page != nil {
		o.page.Close()
		o.page = nil
	}
	if o.b != nil {
		o.b.Close()
		o.b = nil
	}
}

// Normalize collapses whitespace so a re-render does not count as a change.
func Normalize(s string) string { return strings.Join(strings.Fields(s), " ") }

// Compare builds the record for one trial from its two reads.
func Compare(before, after *model.Observation, canary string) model.ObserverRecord {
	rec := model.ObserverRecord{Before: before, After: after}
	if before == nil || after == nil || before.Error != "" || after.Error != "" {
		rec.Detail = "one of the two reads failed, so no change can be judged"
		return rec
	}
	rec.Changed = Normalize(before.Text) != Normalize(after.Text)
	rec.Carries = rec.Changed && evidence.Carries(after.Text, canary) && !evidence.Carries(before.Text, canary)
	switch {
	case !rec.Changed:
		rec.Detail = "state did not change"
	case rec.Carries:
		rec.Detail = "state changed and the new state shows this trial's canary"
	default:
		rec.Detail = "state changed; this trial's canary is not visible in it"
	}
	return rec
}

// Findings turns a record into evidence findings under the target's nonce rule.
//
//	nonce visible in the new state          -> hit (as strong as the nonce)
//	changed, nonce "not_visible" target     -> hit, marked weak (timing only)
//	changed, nonce required but not shown   -> hint only
func Findings(cfg model.ObserverConfig, rec model.ObserverRecord) []model.Finding {
	if !rec.Changed {
		return nil
	}
	switch {
	case rec.Carries:
		return []model.Finding{{Rule: "observer_change", Detail: "observed state changed and shows this trial's canary"}}
	case cfg.Nonce == "not_visible":
		return []model.Finding{{Rule: "observer_change_unattributed", Detail: "observed state changed; the target cannot show a nonce, so the change is attributed by timing alone (weaker than a sink event)"}}
	}
	return []model.Finding{{Rule: "observer_change", Hint: true, Detail: "observed state changed but this trial's canary is not in the new state"}}
}
