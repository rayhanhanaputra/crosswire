package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/rayhanhanaputra/crosswire/internal/cdp"
	"github.com/rayhanhanaputra/crosswire/internal/fakes"
	"github.com/rayhanhanaputra/crosswire/internal/model"
)

// ui drives the real embedded UI in headless Chrome. The tests skip when no
// usable browser is installed.
type ui struct {
	t    *testing.T
	ctx  context.Context
	page *cdp.Page
}

func newUI(t *testing.T, e *env) *ui {
	t.Helper()
	if err := cdp.ProbeForTest(t.Setenv); err != nil {
		t.Skipf("no usable Chrome or Chromium: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	path, _ := cdp.FindChrome()
	b, err := cdp.LaunchWith(ctx, path, "--use-fake-device-for-media-stream", "--use-fake-ui-for-media-stream")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.Close)
	_ = b.GrantMicrophone(ctx, e.srv.URL)
	page, err := b.NewPage(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	_ = page.SetViewport(ctx, 1280, 900)
	if err := page.Navigate(ctx, e.srv.URL+"/#targets"); err != nil {
		t.Fatal(err)
	}
	u := &ui{t: t, ctx: ctx, page: page}
	u.wait(`document.querySelector('#tab-targets select') && document.querySelectorAll('#tab-repeater select option').length >= 0 && window.S === undefined || true`)
	u.wait(`document.querySelector('#ver').textContent.startsWith('version')`)
	return u
}

func (u *ui) wait(expr string) {
	u.t.Helper()
	ctx, cancel := context.WithTimeout(u.ctx, 20*time.Second)
	defer cancel()
	if err := u.page.WaitTrue(ctx, expr); err != nil {
		shot := u.eval(`document.body.innerText.slice(0, 600)`)
		u.t.Fatalf("%v\npage text: %s", err, shot)
	}
}

func (u *ui) eval(expr string) string {
	u.t.Helper()
	v, err := u.page.Eval(u.ctx, expr)
	if err != nil {
		u.t.Fatalf("eval %s: %v", expr, err)
	}
	return v
}

func (u *ui) tab(name string) {
	u.t.Helper()
	u.eval(fmt.Sprintf(`document.querySelector('#tab-btn-%s').click(); true`, name))
	u.wait(fmt.Sprintf(`!document.querySelector('#tab-%s').hidden`, name))
}

// set puts a value into a control found by selector and fires the events a user would.
func (u *ui) set(sel, value string) {
	u.t.Helper()
	js, _ := json.Marshal(value)
	u.eval(fmt.Sprintf(`(function(){var e=document.querySelector(%q); if(!e) throw new Error('no element %s'); e.value=%s; e.dispatchEvent(new Event('input',{bubbles:true})); e.dispatchEvent(new Event('change',{bubbles:true})); return true})()`, sel, strings.ReplaceAll(sel, "'", ""), js))
}

func (u *ui) click(sel string) {
	u.t.Helper()
	u.eval(fmt.Sprintf(`(function(){var e=document.querySelector(%q); if(!e) throw new Error('no element'); e.click(); return true})()`, sel))
}

// labelled finds a control inside a pane by the visible text of its label.
func labelled(root, label string) string {
	return fmt.Sprintf(`(function(){var r=document.querySelector(%q);var ls=[...r.querySelectorAll('label.f,label.check')];var first=function(x){return x.firstChild?(x.firstChild.textContent||'').trim():''};var l=ls.find(function(x){return first(x)===%q})||ls.find(function(x){return (x.textContent||'').trim().startsWith(%q)});return l?l.querySelector('input,select,textarea'):null})()`, root, label, label)
}

func (u *ui) setLabelled(root, label, value string) {
	u.t.Helper()
	js, _ := json.Marshal(value)
	ok := u.eval(fmt.Sprintf(`(function(){var e=%s; if(!e) return 'missing'; if(e.type==='checkbox'){e.checked=%s==='true';} else {e.value=%s;} e.dispatchEvent(new Event('input',{bubbles:true})); e.dispatchEvent(new Event('change',{bubbles:true})); return 'ok'})()`, labelled(root, label), js, js))
	if ok != "ok" {
		u.t.Fatalf("no control labelled %q in %s", label, root)
	}
}

func TestUITargetFormShowsOnlyTheFieldsOfTheChosenTransport(t *testing.T) {
	e := setup(t)
	u := newUI(t, e)
	u.tab("targets")
	visible := func() string {
		return u.eval(`[...document.querySelectorAll('#tab-targets .tgroup[data-for]')].filter(function(g){return !g.hidden && g.offsetParent!==null}).map(function(g){return g.dataset.for}).join(',')`)
	}
	for _, tr := range []string{"http", "ws", "tcp", "mqtt", "voice-ws", "sip", "bridge"} {
		u.set(`#tab-targets form select`, "x") // no-op guard: the first select is Environment
		u.eval(fmt.Sprintf(`(function(){var s=[...document.querySelectorAll('#tab-targets select')].find(function(x){return [...x.options].some(function(o){return o.value==='voice-ws'})}); s.value=%q; s.dispatchEvent(new Event('change',{bubbles:true})); return true})()`, tr))
		got := visible()
		want := tr
		if tr == "http" {
			want = ""
		}
		if got != want {
			t.Errorf("transport %s shows groups %q, want %q", tr, got, want)
		}
	}
	// Observer and mappers live in disclosures that open when they hold a setting.
	if u.eval(`document.querySelector('#sec-observer').open`) != "false" || u.eval(`document.querySelector('#sec-mappers').open`) != "false" {
		t.Error("a new target starts with the optional sections closed")
	}
	u.click(`#sec-observer summary`)
	if u.eval(`document.querySelector('#sec-observer').open`) != "true" {
		t.Error("the observer section opens")
	}
	// Production shows its rule.
	u.eval(`(function(){var s=[...document.querySelectorAll('#tab-targets select')].find(function(x){return [...x.options].some(function(o){return o.value==='production'})}); s.value='production'; s.dispatchEvent(new Event('change',{bubbles:true})); return true})()`)
	if u.eval(`[...document.querySelectorAll('#tab-targets .note')].some(function(n){return !n.hidden && n.textContent.includes('Production targets need TLS')})`) != "true" {
		t.Error("the production note is not shown")
	}
}

func TestUISaveTargetsForEachTransportAndSeeThemInTheAPI(t *testing.T) {
	e := setup(t)
	u := newUI(t, e)
	u.tab("targets")
	pane := "#tab-targets form"
	setTransport := func(v string) {
		u.eval(fmt.Sprintf(`(function(){var s=[...document.querySelectorAll('#tab-targets select')].find(function(x){return [...x.options].some(function(o){return o.value==='voice-ws'})}); s.value=%q; s.dispatchEvent(new Event('change',{bubbles:true})); return true})()`, v))
	}
	newTarget := func(name string) {
		u.eval(`[...document.querySelectorAll('#tab-targets button')].find(function(b){return b.textContent==='New target'}).click(); true`)
		u.setLabelled(pane, "Name", name)
		u.setLabelled(pane, "Forbidden tools", "send_email")
	}
	save := func() {
		u.eval(`[...document.querySelectorAll('#tab-targets button')].find(function(b){return b.textContent==='Save target'}).click(); true`)
		u.wait(`document.querySelector('#toasts').textContent.includes('Target saved') || document.querySelector('#tab-targets .err').textContent !== ''`)
		if msg := u.eval(`document.querySelector('#tab-targets .err').textContent`); msg != "" {
			t.Fatalf("save failed: %s", msg)
		}
		u.eval(`document.querySelector('#toasts').replaceChildren(); true`)
	}
	get := func(name string) model.Target {
		var all []model.Target
		e.json("GET", "/api/targets", nil, 200, &all)
		for _, x := range all {
			if x.Name == name {
				return x
			}
		}
		t.Fatalf("target %q was not saved", name)
		return model.Target{}
	}

	newTarget("ui-ws")
	setTransport("ws")
	u.setLabelled(pane, "Endpoint (perception path)", "ws://127.0.0.1:9/agent")
	u.setLabelled(pane, "Subprotocol", "agent.v1")
	save()
	if x := get("ui-ws"); x.Transport != "ws" || x.WS.Subprotocol != "agent.v1" {
		t.Errorf("ws: %+v", x)
	}

	newTarget("ui-tcp")
	setTransport("tcp")
	u.setLabelled(pane, "Endpoint (perception path)", "tcp://127.0.0.1:9000")
	u.setLabelled(pane, "Framing", "length")
	u.setLabelled(pane, "Read the reply until", "idle")
	u.setLabelled(pane, "Quiet time for idle (ms)", "250")
	save()
	if x := get("ui-tcp"); x.Transport != "tcp" || x.TCP.Framing != "length" || x.TCP.ReadUntil != "idle" || x.TCP.IdleMs != 250 {
		t.Errorf("tcp: %+v", x.TCP)
	}

	newTarget("ui-mqtt")
	setTransport("mqtt")
	u.setLabelled(pane, "Endpoint (perception path)", "mqtt://127.0.0.1:1883")
	u.setLabelled(pane, "Publish topic", "agents/in")
	u.setLabelled(pane, "Response topic", "agents/out")
	u.setLabelled(pane, "QoS", "0")
	u.setLabelled(pane, "Request template", `{"input":"{{content}}","correlation_id":"{{canary}}"}`)
	u.setLabelled(pane, "Username env var NAME", "MQTT_USER")
	save()
	if x := get("ui-mqtt"); x.MQTT.QoS == nil || *x.MQTT.QoS != 0 || x.MQTT.Correlate != "nonce" || x.MQTT.UsernameEnv != "MQTT_USER" {
		t.Errorf("mqtt: %+v", x.MQTT)
	}

	newTarget("ui-bridge")
	setTransport("bridge")
	u.setLabelled(pane, "Endpoint (perception path)", "http://127.0.0.1:9910/action")
	u.eval(`[...document.querySelectorAll('#tab-targets button')].find(function(b){return b.textContent==='Fill defaults for this kind'}).click(); true`)
	save()
	x := get("ui-bridge")
	if x.Transport != "bridge" || x.Bridge.Kind != "led" || x.Bridge.StopEndpoint != "http://127.0.0.1:9910/stop" || x.Bridge.Resource != "pin-9" ||
		len(x.Bridge.AllowedDeviceIDs) != 1 || !strings.Contains(x.RequestTemplate, "blink_units") {
		t.Errorf("bridge defaults: %+v / %s", x.Bridge, x.RequestTemplate)
	}

	newTarget("ui-observer")
	setTransport("http")
	u.setLabelled(pane, "Endpoint (perception path)", "http://127.0.0.1:9/perceive")
	u.click(`#sec-observer summary`)
	u.setLabelled(pane, "Observer", "browser")
	u.setLabelled(pane, "State URL or page URL", "http://127.0.0.1:9/")
	u.setLabelled(pane, "CSS selector (browser)", "#status")
	u.setLabelled(pane, "Nonce in the state", "not_visible")
	u.setLabelled(pane, "Screenshot before and after (browser)", "true")
	u.click(`#sec-mappers summary`)
	u.setLabelled(pane, "Rewrite the message into the target's schema", "true")
	u.setLabelled(pane, "Request template", "{{mapped}}")
	u.setLabelled(pane, "Endpoint", "http://127.0.0.1:9/v1/chat/completions") // the mapper's own field
	u.setLabelled(pane, "Model", "gpt-test")
	u.setLabelled(pane, "API key env var NAME", "OPENAI_API_KEY")
	u.setLabelled(pane, "Instructions", "Return {input}.")
	save()
	x = get("ui-observer")
	if x.Observer.Kind != "browser" || x.Observer.Selector != "#status" || x.Observer.Nonce != "not_visible" || !x.Observer.Screenshot {
		t.Errorf("observer: %+v", x.Observer)
	}
	if !x.Mapper.Enabled || x.Mapper.Model != "gpt-test" || x.Mapper.APIKeyEnv != "OPENAI_API_KEY" || x.Endpoint != "http://127.0.0.1:9/perceive" {
		t.Errorf("mapper: %+v endpoint %s", x.Mapper, x.Endpoint)
	}

	// A bad value comes back as an error under the form, not as a dialog.
	newTarget("ui-bad")
	setTransport("ws")
	u.setLabelled(pane, "Endpoint (perception path)", "http://127.0.0.1:9/x")
	u.eval(`[...document.querySelectorAll('#tab-targets button')].find(function(b){return b.textContent==='Save target'}).click(); true`)
	u.wait(`document.querySelector('#tab-targets .err').textContent.includes('ws://')`)
}

func TestUIRepeaterRunsAWebSocketTargetAndShowsTheTimeline(t *testing.T) {
	e := setup(t)
	agent := &fakes.Agent{Name: "ws-ui"}
	agent.Report = func(ev model.SinkEvent) {
		b, _ := json.Marshal(ev)
		_, _ = http.Post(e.srv.URL+"/api/sink", "application/json", strings.NewReader(string(b)))
	}
	ws := fakes.NewWS(agent)
	defer ws.Close()
	e.json("POST", "/api/targets", map[string]any{"name": "ws-ui", "environment": "staging", "transport": "ws", "endpoint": ws.URL(),
		"request_template": `{"input":"{{content}}"}`, "evidence": map[string]any{"forbidden_tools": []string{"send_email"}}}, 201, nil)
	u := newUI(t, e)
	u.tab("repeater")
	rep := "#tab-repeater"
	u.eval(`(function(){var s=document.querySelector('#tab-repeater select'); var o=[...s.options].find(function(x){return x.textContent.startsWith('ws-ui')}); s.value=o.value; s.dispatchEvent(new Event('change',{bubbles:true})); return true})()`)
	u.eval(`(function(){var s=document.querySelectorAll('#tab-repeater select')[1]; s.value='nfc-tag'; s.dispatchEvent(new Event('change',{bubbles:true})); return true})()`)
	u.wait(`document.querySelector('#tab-repeater textarea')`)
	u.eval(`(function(){var ta=document.querySelector('#tab-repeater textarea'); ta.value='` + fakes.ObeyMarker + ` {{canary}}'; ta.dispatchEvent(new Event('input',{bubbles:true})); return true})()`)
	// The control follows the payload until it is edited.
	if got := u.eval(`document.querySelectorAll('#tab-repeater textarea')[1].value`); !strings.Contains(got, "{{canary}}") || strings.Contains(got, fakes.ObeyMarker) {
		t.Errorf("auto control: %q", got)
	}
	u.set(rep+` input[type=number]`, "5")
	u.eval(`[...document.querySelectorAll('#tab-repeater button')].find(function(b){return b.textContent==='Preview'}).click(); true`)
	u.wait(`document.querySelector('#tab-repeater').textContent.includes('Transport') && document.querySelector('#tab-repeater').textContent.includes('NDEF')`)
	if u.eval(`document.querySelector('#tab-repeater pre.hex') ? document.querySelector('#tab-repeater pre.hex').textContent.length > 20 : false`) != "true" {
		// the hex dump loads asynchronously
		u.wait(`document.querySelector('#tab-repeater pre.hex') && document.querySelector('#tab-repeater pre.hex').textContent.length > 20`)
	}
	u.eval(`[...document.querySelectorAll('#tab-repeater button')].find(function(b){return b.textContent==='Send'}).click(); true`)
	u.wait(`document.querySelector('#tab-repeater .badge.confirmed')`)
	u.wait(`document.querySelectorAll('#tab-repeater tr.detail').length === 10`)
	u.eval(`document.querySelector('#tab-repeater tr.t-injected button.link').click(); true`)
	u.wait(`!document.querySelector('#tab-repeater tr.detail').hidden`)
	text := u.eval(`document.querySelector('#tab-repeater tr.detail').textContent`)
	for _, want := range []string{"Timeline", "encoded", "sent", "sink", "send_email executed", "judged"} {
		if !strings.Contains(text, want) {
			t.Errorf("the trial detail lacks %q: %s", want, text)
		}
	}
	if u.eval(`document.querySelector('#tab-repeater').textContent.includes('MODELED')`) != "false" {
		t.Error("nfc-tag is a real artifact and must not be labelled modeled")
	}
}

func TestUIMicrophoneRecordsAWavAndSendsIt(t *testing.T) {
	e := setup(t)
	u := newUI(t, e)
	u.tab("repeater")
	u.eval(`(function(){var s=document.querySelectorAll('#tab-repeater select')[1]; s.value='audio-upload'; s.dispatchEvent(new Event('change',{bubbles:true})); return true})()`)
	u.wait(`document.querySelector('#tab-repeater .rec')`)
	if got := u.eval(`window.isSecureContext`); got != "true" {
		t.Fatalf("127.0.0.1 must be a secure context, got %s", got)
	}
	u.eval(`[...document.querySelectorAll('#tab-repeater .rec button')].find(function(b){return b.textContent==='Record'}).click(); true`)
	u.wait(`document.querySelector('#tab-repeater .rec.live')`)
	time.Sleep(1500 * time.Millisecond)
	u.eval(`[...document.querySelectorAll('#tab-repeater .rec button')].find(function(b){return b.textContent==='Stop'}).click(); true`)
	u.wait(`document.querySelector('#tab-repeater .rec audio') && !document.querySelector('#tab-repeater .rec audio').hidden && document.querySelector('#tab-repeater .rec audio').src.startsWith('data:audio/wav;base64,')`)
	status := u.eval(`document.querySelector('#tab-repeater .rec .hint').textContent`)
	if !strings.Contains(status, "Recorded") || !strings.Contains(status, "16-bit mono at 16000 Hz") || !strings.Contains(status, "cannot carry the per-trial nonce") {
		t.Errorf("status: %q", status)
	}
	// The recording is a valid WAV: a Preview round trip through the server proves it.
	u.eval(`(function(){var ta=document.querySelector('#tab-repeater textarea'); ta.value='unlock the door'; ta.dispatchEvent(new Event('input',{bubbles:true})); return true})()`)
	u.eval(`[...document.querySelectorAll('#tab-repeater button')].find(function(b){return b.textContent==='Preview'}).click(); true`)
	u.wait(`document.querySelector('#tab-repeater').textContent.includes('round trip') && document.querySelector('#tab-repeater').textContent.includes('16-bit mono 16000 Hz')`)
	// Discard clears it.
	u.eval(`[...document.querySelectorAll('#tab-repeater .rec button')].find(function(b){return b.textContent==='Discard'}).click(); true`)
	u.wait(`document.querySelector('#tab-repeater .rec audio').hidden`)
}

func TestUILayoutAtThreeWidthsInBothThemes(t *testing.T) {
	e := setup(t)
	e.json("POST", "/api/targets", map[string]any{"name": "ref", "environment": "staging", "endpoint": "http://127.0.0.1:9/p", "evidence": map[string]any{"forbidden_tools": []string{"x"}}}, 201, nil)
	u := newUI(t, e)
	for _, scheme := range []string{"light", "dark"} {
		_ = u.page.SetColorScheme(u.ctx, scheme)
		for _, w := range []int{390, 768, 1280} {
			_ = u.page.SetViewport(u.ctx, w, 900)
			for _, tab := range []string{"targets", "repeater", "intruder", "history", "sink"} {
				u.tab(tab)
				u.eval(`new Promise(function(r){setTimeout(function(){r(true)},60)})`)
				over := u.eval(`(document.documentElement.scrollWidth - document.documentElement.clientWidth) + Math.max(0, document.querySelector('main').scrollWidth - document.querySelector('main').clientWidth)`)
				if over != "0" && over != "1" {
					t.Errorf("%s %dpx %s: the page scrolls sideways by %s px: %s", scheme, w, tab, over, u.eval(`'doc '+document.documentElement.scrollWidth+'/'+document.documentElement.clientWidth+' main '+document.querySelector('main').scrollWidth+'/'+document.querySelector('main').clientWidth+' inner '+innerWidth`))
				}
			}
		}
	}
}
