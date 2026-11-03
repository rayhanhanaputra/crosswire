// Package cdp is a small Chrome DevTools Protocol client on the standard
// library and package wsock. It starts an installed Chrome or Chromium in
// headless mode, drives one page at a time and shuts the browser down. It is
// optional at run time: with no browser installed, FindChrome returns an error
// that says so and nothing else in Crosswire is affected.
package cdp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rayhanhanaputra/crosswire/internal/wsock"
)

// ErrNoBrowser is returned when no Chrome or Chromium can be found.
var ErrNoBrowser = errors.New("no Chrome or Chromium found: install one or set CROSSWIRE_CHROME to its path")

// FindChrome returns the browser to launch: CROSSWIRE_CHROME, then well-known
// names on PATH, then the usual install locations, then a Playwright download.
func FindChrome() (string, error) {
	if p := os.Getenv("CROSSWIRE_CHROME"); p != "" {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
		return "", fmt.Errorf("CROSSWIRE_CHROME=%q is not a file", p)
	}
	for _, n := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome", "microsoft-edge", "chrome-headless-shell"} {
		if p, err := exec.LookPath(n); err == nil {
			return p, nil
		}
	}
	var fixed []string
	switch runtime.GOOS {
	case "darwin":
		fixed = []string{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "/Applications/Chromium.app/Contents/MacOS/Chromium", "/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge"}
	case "windows":
		for _, base := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("LocalAppData")} {
			if base != "" {
				fixed = append(fixed, filepath.Join(base, "Google", "Chrome", "Application", "chrome.exe"), filepath.Join(base, "Microsoft", "Edge", "Application", "msedge.exe"))
			}
		}
	default:
		fixed = []string{"/usr/bin/google-chrome", "/usr/bin/chromium", "/snap/bin/chromium", "/opt/google/chrome/chrome"}
	}
	for _, p := range fixed {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		for _, pat := range []string{
			filepath.Join(home, ".cache", "ms-playwright", "chromium-*", "chrome-linux*", "chrome"),
			filepath.Join(home, ".cache", "ms-playwright", "chromium_headless_shell-*", "chrome-headless-shell-linux*", "chrome-headless-shell"),
			filepath.Join(home, "Library", "Caches", "ms-playwright", "chromium-*", "chrome-mac*", "Chromium.app", "Contents", "MacOS", "Chromium"),
		} {
			if m, _ := filepath.Glob(pat); len(m) > 0 {
				return m[len(m)-1], nil
			}
		}
	}
	return "", ErrNoBrowser
}

// Browser is one headless browser process and its DevTools connection.
type Browser struct {
	cmd     *exec.Cmd
	dir     string
	conn    *wsock.Conn
	nextID  atomic.Int64
	mu      sync.Mutex
	pending map[int64]chan rpcReply
	events  map[string]func(sessionID string, params json.RawMessage)
	exited  chan struct{}
	origins map[string]string // session id -> the only origin that page may reach
	closed  chan struct{}
	once    sync.Once
}

type rpcReply struct {
	Result json.RawMessage
	Err    *rpcError
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("devtools: %s (%d)", e.Message, e.Code) }

// Launch starts the browser at path and connects to it.
func Launch(ctx context.Context, path string) (*Browser, error) { return LaunchWith(ctx, path) }

// LaunchWith is Launch with extra command-line flags, for tests that need a
// fake media device.
func LaunchWith(ctx context.Context, path string, extra ...string) (*Browser, error) {
	dir, err := os.MkdirTemp("", "crosswire-chrome-")
	if err != nil {
		return nil, err
	}
	args := []string{"--remote-debugging-port=0", "--user-data-dir=" + dir, "--no-first-run", "--no-default-browser-check",
		"--disable-extensions", "--disable-background-networking", "--disable-sync", "--disable-component-update",
		"--disable-default-apps", "--mute-audio", "--hide-scrollbars", "--window-size=1440,900", "about:blank"}
	if !strings.Contains(strings.ToLower(filepath.Base(path)), "headless") {
		args = append([]string{"--headless=new"}, args...)
	}
	args = append(args, extra...)
	if os.Geteuid() == 0 || os.Getenv("CROSSWIRE_CHROME_NO_SANDBOX") == "1" {
		// Chrome refuses to run as root with its sandbox. CROSSWIRE_CHROME_NO_SANDBOX=1
		// is the opt-in for hosts that block unprivileged user namespaces.
		args = append(args, "--no-sandbox")
	}
	cmd := exec.Command(path, args...)
	stderr := &tailBuffer{max: 4096}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("start %s: %w", filepath.Base(path), err)
	}
	b := &Browser{cmd: cmd, dir: dir, pending: map[int64]chan rpcReply{}, events: map[string]func(string, json.RawMessage){}, origins: map[string]string{}, closed: make(chan struct{})}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	wsURL, err := waitForEndpoint(ctx, dir, exited)
	if err != nil {
		b.kill()
		if strings.Contains(stderr.String(), "No usable sandbox") {
			return nil, errors.New("Chrome could not start its sandbox on this host; set CROSSWIRE_CHROME_NO_SANDBOX=1 to run it without one (only against pages you trust)")
		}
		if tail := strings.TrimSpace(stderr.String()); tail != "" && errors.Is(err, errExited) {
			if i := strings.LastIndex(tail, "\n"); i >= 0 {
				tail = tail[i+1:]
			}
			return nil, fmt.Errorf("%w: %s", err, tail)
		}
		return nil, err
	}
	b.exited = exited
	conn, err := wsock.Dial(ctx, wsURL, nil, wsock.Options{MaxMessage: 32 << 20})
	if err != nil {
		b.kill()
		return nil, fmt.Errorf("connect to DevTools: %w", err)
	}
	b.conn = conn
	go b.readLoop()
	return b, nil
}

var errExited = errors.New("the browser exited before it opened its DevTools port")

type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		t.buf = t.buf[len(t.buf)-t.max:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}

func waitForEndpoint(ctx context.Context, dir string, exited <-chan struct{}) (string, error) {
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(filepath.Join(dir, "DevToolsActivePort")); err == nil {
			lines := strings.Split(strings.TrimSpace(string(data)), "\n")
			if len(lines) >= 2 && lines[0] != "" && strings.HasPrefix(lines[1], "/devtools/") {
				return "ws://127.0.0.1:" + strings.TrimSpace(lines[0]) + strings.TrimSpace(lines[1]), nil
			}
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-exited:
			return "", errExited
		case <-time.After(50 * time.Millisecond):
		}
	}
	return "", errors.New("the browser did not open its DevTools port in 20 s")
}

func (b *Browser) readLoop() {
	defer close(b.closed)
	for {
		_, data, err := b.conn.Read(context.Background())
		if err != nil {
			b.mu.Lock()
			for id, ch := range b.pending {
				ch <- rpcReply{Err: &rpcError{Code: -1, Message: "browser connection closed"}}
				delete(b.pending, id)
			}
			b.mu.Unlock()
			return
		}
		var msg struct {
			ID        int64           `json:"id"`
			Method    string          `json:"method"`
			Params    json.RawMessage `json:"params"`
			SessionID string          `json:"sessionId"`
			Result    json.RawMessage `json:"result"`
			Error     *rpcError       `json:"error"`
		}
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		if msg.ID != 0 {
			b.mu.Lock()
			ch := b.pending[msg.ID]
			delete(b.pending, msg.ID)
			b.mu.Unlock()
			if ch != nil {
				ch <- rpcReply{Result: msg.Result, Err: msg.Error}
			}
			continue
		}
		b.mu.Lock()
		h := b.events[msg.Method]
		b.mu.Unlock()
		if h != nil {
			go h(msg.SessionID, msg.Params)
		}
	}
}

func (b *Browser) on(method string, h func(sessionID string, params json.RawMessage)) {
	b.mu.Lock()
	b.events[method] = h
	b.mu.Unlock()
}

// call sends one command and waits for its reply.
func (b *Browser) call(ctx context.Context, sessionID, method string, params any) (json.RawMessage, error) {
	id := b.nextID.Add(1)
	ch := make(chan rpcReply, 1)
	b.mu.Lock()
	b.pending[id] = ch
	b.mu.Unlock()
	req := map[string]any{"id": id, "method": method}
	if params != nil {
		req["params"] = params
	}
	if sessionID != "" {
		req["sessionId"] = sessionID
	}
	data, _ := json.Marshal(req)
	if err := b.conn.WriteText(ctx, data); err != nil {
		return nil, err
	}
	select {
	case r := <-ch:
		if r.Err != nil {
			return nil, r.Err
		}
		return r.Result, nil
	case <-ctx.Done():
		b.mu.Lock()
		delete(b.pending, id)
		b.mu.Unlock()
		return nil, ctx.Err()
	}
}

// Page is one tab.
type Page struct {
	b        *Browser
	session  string
	targetID string
	origin   string
}

// NewPage opens a tab. When origin is not empty every request outside that
// origin (scheme, host and port) is blocked, except data:, blob: and about:.
func (b *Browser) NewPage(ctx context.Context, origin string) (*Page, error) {
	res, err := b.call(ctx, "", "Target.createTarget", map[string]any{"url": "about:blank"})
	if err != nil {
		return nil, err
	}
	var ct struct {
		TargetID string `json:"targetId"`
	}
	_ = json.Unmarshal(res, &ct)
	res, err = b.call(ctx, "", "Target.attachToTarget", map[string]any{"targetId": ct.TargetID, "flatten": true})
	if err != nil {
		return nil, err
	}
	var at struct {
		SessionID string `json:"sessionId"`
	}
	_ = json.Unmarshal(res, &at)
	p := &Page{b: b, session: at.SessionID, targetID: ct.TargetID, origin: origin}
	if _, err := b.call(ctx, p.session, "Page.enable", nil); err != nil {
		return nil, err
	}
	if _, err := b.call(ctx, p.session, "Runtime.enable", nil); err != nil {
		return nil, err
	}
	if origin != "" {
		b.mu.Lock()
		b.origins[p.session] = origin
		b.mu.Unlock()
		b.on("Fetch.requestPaused", b.requestPaused)
		if _, err := b.call(ctx, p.session, "Fetch.enable", map[string]any{"patterns": []map[string]string{{"urlPattern": "*"}}}); err != nil {
			return nil, err
		}
	}
	return p, nil
}

// requestPaused lets a page's own origin through and fails everything else.
func (b *Browser) requestPaused(sid string, params json.RawMessage) {
	var ev struct {
		RequestID string `json:"requestId"`
		Request   struct {
			URL string `json:"url"`
		} `json:"request"`
	}
	if json.Unmarshal(params, &ev) != nil {
		return
	}
	b.mu.Lock()
	origin := b.origins[sid]
	b.mu.Unlock()
	cctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if origin == "" || sameOrigin(ev.Request.URL, origin) {
		_, _ = b.call(cctx, sid, "Fetch.continueRequest", map[string]any{"requestId": ev.RequestID})
		return
	}
	_, _ = b.call(cctx, sid, "Fetch.failRequest", map[string]any{"requestId": ev.RequestID, "errorReason": "BlockedByClient"})
}

func sameOrigin(raw, origin string) bool {
	if strings.HasPrefix(raw, "data:") || strings.HasPrefix(raw, "blob:") || strings.HasPrefix(raw, "about:") {
		return true
	}
	a, err1 := url.Parse(raw)
	b, err2 := url.Parse(origin)
	if err1 != nil || err2 != nil {
		return false
	}
	return a.Scheme == b.Scheme && a.Host == b.Host
}

// Origin returns scheme://host of a URL, for NewPage.
func Origin(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// Navigate loads url and waits for the document to be interactive.
func (p *Page) Navigate(ctx context.Context, rawURL string) error {
	res, err := p.b.call(ctx, p.session, "Page.navigate", map[string]any{"url": rawURL})
	if err != nil {
		return err
	}
	var nav struct {
		ErrorText string `json:"errorText"`
	}
	_ = json.Unmarshal(res, &nav)
	if nav.ErrorText != "" {
		return fmt.Errorf("navigation failed: %s", nav.ErrorText)
	}
	for {
		v, err := p.Eval(ctx, `document.readyState`)
		if err == nil && (v == "interactive" || v == "complete") {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("page did not load: %w", ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// Eval evaluates an expression and returns its string value ("" for null or undefined).
func (p *Page) Eval(ctx context.Context, expr string) (string, error) {
	res, err := p.b.call(ctx, p.session, "Runtime.evaluate", map[string]any{"expression": expr, "returnByValue": true, "awaitPromise": true})
	if err != nil {
		return "", err
	}
	var out struct {
		Result struct {
			Type  string          `json:"type"`
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		Exception *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return "", err
	}
	if out.Exception != nil {
		return "", fmt.Errorf("script error: %s", out.Exception.Text)
	}
	if len(out.Result.Value) == 0 || string(out.Result.Value) == "null" {
		return "", nil
	}
	var s string
	if json.Unmarshal(out.Result.Value, &s) == nil {
		return s, nil
	}
	return string(out.Result.Value), nil
}

// Location is the page's current URL.
func (p *Page) Location(ctx context.Context) (string, error) {
	return p.Eval(ctx, `location.href`)
}

// Title is the page title.
func (p *Page) Title(ctx context.Context) (string, error) { return p.Eval(ctx, `document.title`) }

// VisibleText waits until the first element matching the selector is visible
// and returns its text. It never navigates.
func (p *Page) VisibleText(ctx context.Context, selector string) (string, error) {
	sel, _ := json.Marshal(selector)
	expr := `(function(){var e=document.querySelector(` + string(sel) + `);if(!e)return null;var r=e.getBoundingClientRect();return (r.width>0&&r.height>0)?e.innerText:null;})()`
	for {
		if v, err := p.evalRaw(ctx, expr); err == nil && v != nil {
			return *v, nil
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("selector %q was not visible: %w", selector, ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (p *Page) evalRaw(ctx context.Context, expr string) (*string, error) {
	res, err := p.b.call(ctx, p.session, "Runtime.evaluate", map[string]any{"expression": expr, "returnByValue": true})
	if err != nil {
		return nil, err
	}
	var out struct {
		Result struct {
			Value *string `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return nil, err
	}
	return out.Result.Value, nil
}

// Screenshot returns the page as PNG, whole page rather than the viewport.
func (p *Page) Screenshot(ctx context.Context, maxBytes int) ([]byte, error) {
	res, err := p.b.call(ctx, p.session, "Page.captureScreenshot", map[string]any{"format": "png", "captureBeyondViewport": true})
	if err != nil {
		return nil, err
	}
	var out struct {
		Data string `json:"data"`
	}
	_ = json.Unmarshal(res, &out)
	png, err := base64.StdEncoding.DecodeString(out.Data)
	if err != nil {
		return nil, errors.New("screenshot was not valid base64")
	}
	if maxBytes > 0 && len(png) > maxBytes {
		return nil, fmt.Errorf("screenshot is %d bytes; the limit is %d", len(png), maxBytes)
	}
	return png, nil
}

// Close closes the tab.
func (p *Page) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _ = p.b.call(ctx, "", "Target.closeTarget", map[string]any{"targetId": p.targetID})
}

// Call sends a raw DevTools command on the page's session. It exists for tests
// that need to grant permissions or inject media devices.
func (p *Page) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return p.b.call(ctx, p.session, method, params)
}

// CallBrowser sends a raw command to the browser itself.
func (b *Browser) CallBrowser(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return b.call(ctx, "", method, params)
}

func (b *Browser) kill() {
	if b.cmd != nil && b.cmd.Process != nil {
		_ = b.cmd.Process.Kill()
	}
	_ = os.RemoveAll(b.dir)
}

// Close shuts the browser down and removes its profile directory.
func (b *Browser) Close() {
	b.once.Do(func() {
		if b.conn != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_, _ = b.call(ctx, "", "Browser.close", nil)
			cancel()
			_ = b.conn.Close()
		}
		select {
		case <-b.exited:
		case <-time.After(3 * time.Second):
			_ = b.cmd.Process.Kill()
			<-b.exited
		}
		_ = os.RemoveAll(b.dir)
	})
}

// Probe finds a browser, starts it and closes it again. Tests and selftest use
// it to learn whether browser checks can run on this host.
func Probe(ctx context.Context) error {
	path, err := FindChrome()
	if err != nil {
		return err
	}
	b, err := Launch(ctx, path)
	if err != nil {
		return err
	}
	b.Close()
	return nil
}

// ProbeForTest is Probe for tests: on a host that blocks Chrome's sandbox it
// opts in to --no-sandbox through setenv (the pages are local fakes) and tries
// once more. It returns the reason a browser test must be skipped, or nil.
func ProbeForTest(setenv func(key, value string)) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err := Probe(ctx)
	if err != nil && strings.Contains(err.Error(), "CROSSWIRE_CHROME_NO_SANDBOX") {
		setenv("CROSSWIRE_CHROME_NO_SANDBOX", "1")
		err = Probe(ctx)
	}
	return err
}

// WaitTrue polls a JavaScript expression until it is truthy or ctx ends.
func (p *Page) WaitTrue(ctx context.Context, expr string) error {
	for {
		v, err := p.Eval(ctx, "Boolean("+expr+")")
		if err == nil && v == "true" {
			return nil
		}
		select {
		case <-ctx.Done():
			if err != nil {
				return fmt.Errorf("waiting for %s: %w (%v)", expr, ctx.Err(), err)
			}
			return fmt.Errorf("waiting for %s: %w", expr, ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// SetViewport sets the page size in CSS pixels.
func (p *Page) SetViewport(ctx context.Context, width, height int) error {
	_, err := p.Call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": width, "height": height, "deviceScaleFactor": 1, "mobile": width < 500})
	return err
}

// SetColorScheme emulates prefers-color-scheme: "light" or "dark".
func (p *Page) SetColorScheme(ctx context.Context, scheme string) error {
	_, err := p.Call(ctx, "Emulation.setEmulatedMedia", map[string]any{"features": []map[string]string{{"name": "prefers-color-scheme", "value": scheme}}})
	return err
}

// GrantMicrophone lets origin use the microphone without a prompt.
func (b *Browser) GrantMicrophone(ctx context.Context, origin string) error {
	_, err := b.CallBrowser(ctx, "Browser.grantPermissions", map[string]any{"origin": origin, "permissions": []string{"audioCapture"}})
	return err
}
