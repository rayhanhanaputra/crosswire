# Testing every transport locally

Crosswire ships in-process fakes for every transport: a WebSocket agent, a TCP agent,
an MQTT 3.1.1 broker with an agent behind it, a realtime voice service, a device
bridge, a state page for the observers, and a dummy OpenAI-compatible mapper. They run
inside the test binary or inside `crosswire selftest`. **No network, no hardware, no
API key, nothing to start in another terminal.**

## The two ways to run them

```bash
go test ./...          # the full matrix, as Go tests
./crosswire selftest   # one trial set per transport, as a table
```

`selftest` takes `--repeats N` (default 5). Fewer than 5 repeats cannot separate the
Wilson intervals, so rows that should be `confirmed` come out `observed` and the table
reports them as failures — that is the verdict rule working, not a broken fake. It
exits non-zero if any row fails.

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
browser observer            text          -         -        skipped    skipped  no usable Chrome or Chromium
input + response mapper     text          5/5       0/5      confirmed  pass     10/10 trials mapped both ways; mapped fields are labelled and are never evidence

11 passed, 0 failed, 1 skipped
```

Two rows are worth reading closely, because they assert the *rules* rather than the
plumbing:

- **`observer, no nonce visible` must come out `observed`, not `confirmed`.** The state
  page changes, but it cannot show which trial changed it, so the hits are marked weak
  and the run is not allowed to reach `confirmed`.
- **`input + response mapper` is `confirmed` by the sink, not by the mapper.** The note
  records that both mappers ran on every trial; the verdict would be identical without
  them, because mapped fields are never evidence.

## The browser rows

The browser observer and the UI tests drive an installed Chrome or Chromium over the
DevTools protocol. With none installed they **skip** — they do not fail. To run them,
install one and, if it is not in a standard location, point `CROSSWIRE_CHROME` at the
executable. On a host that blocks the browser sandbox, set
`CROSSWIRE_CHROME_NO_SANDBOX=1`.

```bash
CROSSWIRE_CHROME=/Applications/Chromium.app/Contents/MacOS/Chromium \
  go test ./internal/server/ -run UI
```

## Driving a transport by hand

To click through the UI against something real, run a reference target — it is the
same shape your own agent would be, and it needs a model key:

```bash
export OPENAI_API_KEY=...
./crosswire target home-agent      # :7201, vulnerable
./crosswire serve                  # another terminal
```

Then **Targets** → **Load reference target** → `home-agent`, and work through
[adding-a-target.md](adding-a-target.md). For a target with no model at all, the Go
tests are the better tool: `internal/fakes/` has an agent that obeys any instruction it
is given, which is what the table above exercises.

## What is not covered

Only the *simulated* bridge paths are tested. A real LED rig, radio or NFC reader is
driven by a helper program you supply (`HELPER start` / `HELPER stop`, JSON on stdin
and stdout; [`../CONTRACT.md`](../CONTRACT.md) section 10.1). The SIP connector has
been run against a lab endpoint, not a product, and has no TLS or SRTP.
