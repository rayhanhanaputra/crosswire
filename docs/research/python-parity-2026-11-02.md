# Crosswire parity report: Python workbench port

> **Historical record.** Written while the Python workbench still existed, before the
> repository was renamed to Crosswire and the Go tree moved to the root. Paths below
> that begin `crosswire/` are now at the repository root, and `harness/`, `web/` and
> `tests/` have since been removed. Kept for the parity argument, not as current
> layout.

Date: 2026-11-02. Nothing is committed. `harness/`, `web/`, the results, the paper and the decks are untouched. All paths are under the `crosswire/` subdirectory unless noted.

## Result

Crosswire now covers every connector, channel, rig, mapper, observer, safeguard and local test of the Python workbench. `go vet ./...` is clean. `go test -race ./...` passes (236 passed, 0 failed, 6 skipped; the 6 are the old `TestLive*` tests that need `CROSSWIRE_LIVE=1`). `go.mod` and `go.sum` are unchanged: **no new dependencies**.

Real hardware (LED rig, BLE radio, NFC reader, GPIO) was **not tested**. Only the simulate paths are tested. See "Not done".

## Feature parity table

"Tested how" names the harness. "Real HW" is whether it ran against real hardware or a real remote service.

| Python feature | Crosswire equivalent | Files | Tested how | Real HW |
| --- | --- | --- | --- | --- |
| HTTP connector | `http` transport, now with guarded dial, `secret_headers`, reply-audio path | `internal/connector/http.go`, `replyaudio.go` | existing http tests, `transports_test.go` (reply audio), selftest | no (loopback fakes) |
| WebSocket connector | `ws` transport (one text message out, first reply in, subprotocol, wss) | `connector/ws.go`, `internal/wsock/wsock.go`, `fakes/ws.go` | `connector/transports_test.go` (round trip, headers, secret header, subprotocol, timeout, wss with self-signed cert, refusals), `wsock_test.go`, engine e2e, selftest | no |
| TCP connector | `tcp` transport: json or raw send, terminator or 4-byte length framing, read until terminator, close, idle or none, TLS, reply cap, private networks only | `connector/tcp.go`, `fakes/tcp.go` | `transports_test.go` (line, raw CRLF, length, idle, close, none, silent, cap, public refused, TLS trusted and untrusted, terminator table), engine e2e, selftest | no |
| MQTT connector (paho) | own MQTT 3.1.1 client: QoS 0/1, TLS (`mqtts://`), username and password from env var NAMES, `{{canary}}` in topics, reply matched by nonce, JSON path or first | `connector/mqtt.go`, `fakes/mqtt.go` (broker and agent) | `transports_test.go` (nonce with noise, path, first, QoS 0, canary topics, auth from env, wrong password, TLS, timeout, wildcard, QoS 2, public broker), engine e2e, selftest | no |
| Voice HTTP connector | `http` transport with `speech`/`dtmf`/`audio-upload` and `{{audio_base64}}`; `reply_audio_path` keeps reply audio | `connector/replyaudio.go`, `channel/extra.go` | `TestReplyAudioIsKept...`, engine voice test | no |
| Voice-stream (WebSocket media) | `voice-ws` transport: start event, WAV in chunks, end event, reply events to a done marker, reply audio kept, cancel on failure, 6 MiB caps | `connector/voicews.go`, `fakes/voicews.go` | `transports_test.go` (chunk count, reply audio, no audio, oversize, bad base64, stall timeout, event limit sends cancel), engine e2e, selftest | no |
| Optical HTTP connector | not a separate connector: `http` + image adapter (`qr`, `sign`, `ocr-text`, `invoice`) + `{{image_base64}}` | documented in CONTRACT 1.1 | existing and new http/channel tests | no |
| Device bridge connector (action, stop, allowlists, before/after, ack, stop confirmation) | `bridge` transport; checks first, action, hold for observer, stop always, halt run on unconfirmed stop | `connector/bridge.go`, `engine/extras.go`, `fakes/bridge.go` | `connector/bridge_test.go` (12 pre-send refusals, order, stop on cancel, stop failure, refused action), `engine/transports_test.go` (serial run, timeline, halt, ack, channel match) | **no** |
| `aat-arduino-bridge` (HTTP to serial, simulate) | `crosswire bridge --simulate / --serial-port / --exec` | `internal/bridge/bridge.go`, `cmd.go`, `rig/rig.go` (`Play`) | `bridge/bridge_test.go` (validation, 502 on firmware refusal, serial driver against simulated firmware, exec helper via `sh`, loopback-only listen) | **no** (serial and helper untested on hardware) |
| BLE, NFC, GPIO operations | `ble`, `nfc`, `gpio` bridge kinds through an external helper (`HELPER start` / `HELPER stop`, JSON on stdin and stdout) | `bridge/bridge.go` (`Exec`) | helper protocol tested with shell helpers | **no** |
| Observer: HTTP | `observer.kind: http` (state endpoint, JSON path, secret headers, reset URL) | `internal/observer/observer.go` | `observer_test.go`, engine tests | no |
| Observer: browser (Playwright) | `observer.kind: browser` on an own DevTools client over an installed Chrome/Chromium; text, screenshot, live page, same-origin only | `internal/cdp/cdp.go`, `observer.go` | `observer_test.go`, `engine/observer_mapper_test.go`, UI tests, selftest, all against real headless Chromium 1248 | yes for the browser (a real Chromium); no for any production page |
| LLM input mapper | `mapper` + `{{mapped}}` | `internal/mapper/mapper.go`, `engine/extras.go` | `mapper_test.go`, engine tests (shapes request, drops canary, failure, missing key) | no (fake chat server) |
| LLM response mapper | `response_mapper` | same | `mapper_test.go`, engine tests | no |
| Production acknowledgement | engine refuses without `production_acknowledged`; also for bridge targets | `engine/engine.go`, `extras.go` | engine tests | n/a |
| Concurrency 1 for production and hardware | engine forces 1 for production, observer, bridge | `engine/engine.go` | engine bridge test (start/stop alternate) | n/a |
| HTTPS or WSS off-host | `CheckProduction` for production targets (https, wss, mqtts, tcp+tls; plain SIP refused off-host); bridge and observer and mapper URLs need TLS off-host always | `connector/production.go`, `server/transports.go` | engine and server tests (4 production refusals on run, 4 on save) | n/a |
| Unsafe destination addresses refused | `guard.DialContext` pins the checked IP; refuses unspecified, multicast, link-local, reserved; raw TCP and MQTT private only | `internal/guard/guard.go` | `guard_test.go`, connector tests (metadata address refused on ws, observer, tcp) | n/a |
| Redirects refused, size and time limits | all connectors, observers, mappers | connectors | tests per transport | n/a |
| No credentials in URLs, managed or credential headers refused, secrets as env var names | server validation | `server/targets.go`, `transports.go` | `TestTargetTransportValidation` (35 bad cases, 12 good) | n/a |
| Hardware allowlists, mandatory stop, cleanup | `CheckBridge`, stop after every action | `connector/bridge.go` | as above | no |
| Inconclusive without deterministic rules | `invalid` (unless an observer is set) | `evidence/evidence.go` | evidence tests | n/a |
| Channels: prompt-text, json-field, sensor-value, reading-stream, binary-toggle, morse-blink | `text`, `json-fields`, `sensor-value` (modeled), `reading-stream` (modeled), `binary-toggle` (pulses, GPIO state), `morse-led` (now also exposes `blink_units`) | `channel/extra.go`, `signal/toggle.go` | `channel/extra_test.go`, `signal/artifacts_test.go` | no |
| Channels: ble-advert, nfc-tag | `ble-advert` (Flags, Complete Local Name, manufacturer data, legacy 31 B or extended 254 B, decoded back), `nfc-tag` (NDEF text and URI records, NTAG capacity, decoded back) | `signal/ble.go`, `signal/ndef.go` | `artifacts_test.go`, `extra_test.go`, engine e2e (nfc over ws), bridge ble test | no (bytes are real, nothing was radiated) |
| Channels: text-in-image, qr-code | `ocr-text` (rendered PNG), `qr` (existing) | `channel/extra.go` | tests | no |
| Channels: agent-note, retrieved-doc, voice-transcript | `agent-note`, `retrieved-doc`, `voice-transcript`, all labelled `modeled`; `handoff` for stored notes | `channel/extra.go` | `TestModeledTextAdapters` | n/a |
| Audio upload | `audio-upload` (WAV validated: format, size, rate; refuses a target that requires the canary) | `channel/extra.go`, `signal/wavinfo.go` | `TestAudioUpload`, `TestAudioUploadNeedsAPermissiveTarget` | no |
| Microphone capture | Record button in the Repeater: `getUserMedia` + Web Audio to 16 kHz 16-bit mono WAV, preview, discard, 60 s cap, secure-origin notice; `Permissions-Policy: microphone=(self)` | `server/ui/app.js`, `server.go` | `TestUIMicrophoneRecordsAWavAndSendsIt` in headless Chromium with Chrome's fake audio device; the recorded WAV round-trips through the server preview | no (fake device, not a real microphone) |
| Workbench, Campaigns, Targets, History tabs | Repeater, Intruder, Targets, History (existing), now with all fields above | `server/ui/*` | `ui_test.go` (5 tests) | n/a |
| Local connector tests | in-process fakes for every connector, `go test ./...`, and `crosswire selftest` | `internal/fakes`, `internal/selftest`, `cmd/crosswire/main.go` | see below | n/a |

## What was improved beyond parity

- **Observer evidence follows the sink's rules.** A change counts only when it carries the trial's nonce; the control arm is observed too and any control change voids the run; a change with no visible nonce is marked weak, counted separately (`weak_hits`), and cannot confirm a run. Documented in CONTRACT section 8. Python had no nonce and no control arm for observers.
- **Mapper output is never evidence.** Mapped fields are stored beside the deterministic ones and labelled MAPPED. A forbidden action only the response mapper read is a hint. If the input mapper drops the canary from most injected requests the run is `invalid`.
- **Observers work on any transport** (Python tied them to the bridge), and have a reset URL and a live-page mode.
- **Bridge halts the run on an unconfirmed stop**, sends the stop on cancel without using the run's context, and holds the stimulus active while the observer reads (Python read after the action and stopped after).
- **Trial timeline** for every trial: encode, mapper, observer reads, send, bridge ack and stop, response, trigger, each sink event, judged.
- **Kill switch**: `POST /api/stop` and a "Stop all" button cancel every run in flight.
- **Pinned, checked destinations** on every connection (link-local and cloud metadata blocked), now also for HTTP, which Crosswire did not check before.
- **Serial and helper bridge in one binary** with no cgo: `crosswire bridge`. BLE, NFC and GPIO go through a helper, so no Bluetooth or NFC library is linked.
- **Data-driven UI fields** (`kinds` in the channel catalogue), per-transport target form groups, trial detail panels, screenshots in the run and the bundle.
- **Fixed a layout bug found on the way:** at 390 px the long data-directory pill in the header widened the whole page (body grid column was `auto`).
- Browser start failures are reported plainly (no browser, no usable sandbox) instead of timing out.

## New dependencies and why

None. The WebSocket client (`internal/wsock`), the MQTT 3.1.1 client, the DevTools client (`internal/cdp`) and the fakes are written on the standard library. That keeps `CGO_ENABLED=0` cross builds trivial and avoids a toolchain bump: the current chromedp needs Go 1.25 and this module is on 1.22. The task allowed chromedp as an example, not a requirement.

## Selftest output

`CROSSWIRE_CHROME_NO_SANDBOX=1 ./crosswire selftest` (no network, no hardware; exit 0, about 22 s). Without that variable on this host, the browser row is skipped with the message shown below, because this machine blocks Chrome's sandbox.

```text
CONNECTOR                   CHANNEL       INJECTED  CONTROL  OUTCOME    RESULT   NOTE
http                        qr            5/5       0/5      confirmed  pass     QR rendered and decoded
websocket                   json-fields   5/5       0/5      confirmed  pass     request/response session
tcp                         text          5/5       0/5      confirmed  pass     JSON line, newline terminator
tcp                         text          5/5       0/5      confirmed  pass     raw text, 4-byte length framing
mqtt 3.1.1                  sensor-value  5/5       0/5      confirmed  pass     QoS 1, reply matched by nonce, unrelated reply skipped
voice over websocket        audio-upload  5/5       0/5      confirmed  pass     WAV streamed in 1 KiB chunks, reply audio kept
device bridge (led)         morse-led     5/5       0/5      confirmed  pass     Morse pulses, stop confirmed after every action, http observer
device bridge (ble)         ble-advert    5/5       0/5      confirmed  pass     BLE advertising data, simulated radio
http + http observer        ocr-text      5/5       0/5      confirmed  pass     state change shows the trial's nonce
observer, no nonce visible  text          5/5       0/5      observed   pass     hits are weak, so the run cannot be confirmed
browser observer            text          5/5       0/5      confirmed  pass     headless Chrome, selector text and screenshot before and after
input + response mapper     text          5/5       0/5      confirmed  pass     10/10 trials mapped both ways; mapped fields are labelled and are never evidence

12 passed, 0 failed, 0 skipped
```

Without the variable the browser row reads: `skipped  Chrome could not start its sandbox on this host; set CROSSWIRE_CHROME_NO_SANDBOX=1 to run it without one (only against pages you trust)`.

## go test summary

- `go vet ./...`: clean (also `GOOS=darwin GOARCH=arm64` and `GOOS=windows GOARCH=amd64`).
- `gofmt -l .`: clean.
- `go test -count=1 -race ./...`: all packages ok. 236 top-level tests passed, 0 failed, 6 skipped (the old live-model tests, needing `CROSSWIRE_LIVE=1`). About 41 s with the race detector.
- New packages with tests: `guard`, `wsock`, `mapper`, `observer`, `bridge`, `selftest`; new tests in `connector`, `channel`, `signal`, `engine`, `evidence`, `rig`, `server`.
- Browser tests (observer, UI, selftest row) ran against the Playwright Chromium in `~/.cache/ms-playwright`. They set `CROSSWIRE_CHROME_NO_SANDBOX=1` themselves for the test process only, and skip when no usable browser exists.
- Existing tests: all still green. Two assertions were updated for the new behaviour: the channel count in `TestHealthAndChannels` (12 to 23) and the health body type (it now also reports `allow_remote`).

## Cross-compile results (`CGO_ENABLED=0`)

| Target | Result |
| --- | --- |
| darwin/arm64 | builds, 12.8 MB |
| windows/amd64 | builds, 13.6 MB |
| linux/arm64 | builds, 12.7 MB |
| linux/amd64 | builds, 13.3 MB |

## Detector

`node ~/.claude/skills/craft/scripts/detect.mjs crosswire/internal/server/ui/` exits 0. One hit on the way (a width transition on the microphone level bar) was fixed with a `transform: scaleX` instead. No native `alert`, `confirm` or `prompt` in the UI (grep clean); confirmations use the existing `uiConfirm` dialog, including a new "Drive the hardware?" one.

Delivery gate, as recorded by `ui_test.go` and by screenshots (`webtool browse`) in the scratchpad:

- `TestUILayoutAtThreeWidthsInBothThemes`: all five tabs at 390, 768 and 1280 px, light and dark, no sideways page scroll.
- `TestUITargetFormShowsOnlyTheFieldsOfTheChosenTransport`: each of the 7 transports shows exactly its settings group; production shows its TLS note; optional sections start closed.
- `TestUISaveTargetsForEachTransportAndSeeThemInTheAPI`: ws, tcp, mqtt, bridge (the "Fill defaults" button), browser observer and input mapper saved through the form and read back from the API; a bad value shows an inline error.
- `TestUIRepeaterRunsAWebSocketTargetAndShowsTheTimeline`: target-aware adapter list, auto control, Preview (transport block, NDEF hex dump), Send, verdict badge, trial detail with timeline.
- `TestUIMicrophoneRecordsAWavAndSendsIt`: Record, Stop, player with a data URL, status text, round trip through Preview, Discard.
- Keyboard: tabs and dialogs keep the existing keyboard handling; the new controls are native inputs, buttons and `details`. I did not run a separate keyboard walk-through.

## Not done, caveats and things to push back on

- **Real hardware is untested.** LED rig over a real serial port, BLE radio, NFC reader, GPIO helper. Tested instead: the simulate backend, the HTTP protocol, the serial driver against a simulated firmware, and the helper protocol against shell helpers. No BLE, NFC or GPIO helper is shipped; the protocol is documented (CONTRACT 10.1).
- **Microphone** tested only with Chrome's fake audio device. It uses `ScriptProcessorNode` (deprecated but supported everywhere). Firefox and Safari are untested. Over `--allow-remote` the browser blocks the microphone unless the page is HTTPS; the UI says so.
- **Browser observer** needs Chrome, Chromium or Edge. Discovery on macOS and Windows paths is written but untested. On hosts that block Chrome's sandbox it fails with a message unless `CROSSWIRE_CHROME_NO_SANDBOX=1` is set (opt-in on purpose; Chrome runs with its sandbox by default).
- **MQTT:** 3.1.1 only. No MQTT 5, no QoS 2, no retained messages, no last will. TLS tested with a self-signed fake, not a real broker.
- **WebSocket `ws` transport** takes the first reply message only, as Python did. Multi-event replies are handled by `voice-ws`.
- **Optical HTTP** is not a separate connector (it did not differ); it is `http` plus an image adapter. Documented.
- **Not carried over:** `{{body}}` and `{{message}}` placeholders, importing the Python SQLite run database, the Playwright dependency.
- **Behaviour changes to existing targets, on save or send:** `Authorization`, `Cookie` and similar are refused in plain `headers` (use `secret_headers`); credentials and fragments in URLs are refused; HTTP targets can no longer be at an unspecified (0.0.0.0), link-local or reserved address; the CSP now also allows `blob:` for the recorder's preview; `/api/health` gains `allow_remote`. A stored target is unaffected until it is saved again, except for the address check and the production TLS check, which also run at send time.
- **Raw TCP and MQTT** are limited to loopback and private networks, as in Python, with one deviation: the 100.64.0.0/10 range (NetBird, Tailscale) counts as private.
- **Observer limits:** a state that stays changed after a hit hides the next change unless the target offers a reset URL. This is documented; I did not add retries.
- **Intruder with `json-fields`:** the payload is substituted into JSON text, so a payload with quotes breaks the JSON. Documented only.
- **Production** has no per-target rate limit, request budget or audit log. Python did not either; the task said port what exists.
- The `docs/results` tree, paper and decks were not touched. The `README.md` Legacy section was reworded to say the Python workbench can be retired, and points at the new migration table in `docs/USAGE.md`; the legacy sections below it are unchanged.

## Files

New: `internal/{wsock,guard,mapper,observer,cdp,bridge,fakes,selftest}/`, `internal/connector/{ws,tcp,mqtt,voicews,bridge,preview,production,replyaudio}.go`, `internal/channel/extra.go`, `internal/signal/{ndef,ble,wavinfo,toggle}.go`, `internal/engine/extras.go`, `internal/server/transports.go`, and their tests (`connector/transports_test.go`, `engine/{transports,observer_mapper}_test.go`, `server/{transports,ui}_test.go`, `channel/extra_test.go`, `signal/artifacts_test.go`).

Changed: `cmd/crosswire/main.go`, `internal/model/model.go`, `internal/connector/http.go`, `internal/channel/channel.go`, `internal/engine/engine.go`, `internal/evidence/evidence.go`, `internal/rig/rig.go`, `internal/server/{server,runs,targets,jobs}.go`, `internal/server/ui/{app.js,app.css,index.html}`, `docs/CONTRACT.md` (sections 1.1 to 1.3, 4, 5, and new 8 to 11), `README.md` (this directory), and `../README.md`.
