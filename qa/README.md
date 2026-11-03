# Integration QA

`go test ./...` tests the packages. This suite tests the **built binary**: it starts
the real workbench, the real reference targets with their agent loop and defence
layers, and a real device bridge as separate processes, then drives them over the
HTTP API the way a user would and asserts on the verdicts the engine produced.

```bash
zsh qa/run.sh
```

Needs Go and zsh. **No network, no API key, no hardware.** It builds what it needs,
clears the ports it uses, runs the suite and prints a report:

```text
================ RESULT:  10 passed, 0 failed ================
```

It exits non-zero if any case fails.

## The fake model

The reference targets are real tool-calling agents, so they need an
OpenAI-compatible endpoint. `fakemodel.go` is a deterministic stand-in: a gullible
model that reads everything it was given — system prompt included, because that is
where the home agent's entity list arrives — and calls any tool whose name appears
in that text, lifting the argument out of the text.

That is deliberate. A model that always obeys moves the question off the model and
onto the thing under test: **the defence layers and the evidence rules**. If a
hardened build still executes nothing against a model that never refuses, the layer
is what stopped it.

The same binary also answers the tool-less calls the quarantine model and the
classifier make.

## What each case covers

| Case | What it asserts |
| ---- | --------------- |
| A1 | The vulnerable `home-agent` reaches `confirmed`: injected 5/5, control 0/5, intervals separated. The full agent loop, the sink and the verdict rule, end to end. |
| A2 | The same payload against `--policy hardened` executes nothing. `attempted` is reported separately, so a code gate reads differently from a model that declined. |
| B1 | Two-phase store-then-trigger on `desk-agent`: the note is planted on `/perceive`, read on a later `/chat`, and the timeline shows both exchanges. |
| C1–C4 | A real `crosswire bridge` process: the run reaches it, an emergency stop is confirmed **even when the action is refused**, a run without acknowledgement is refused, and a sequence past the duration cap is refused. |
| D1–D3 | Failure paths: an unreachable target is `error` and never a finding; a target with no evidence rules is `invalid`; a payload that cannot carry the canary is refused. |
| E | Three runs at once against one target: every verdict holds, and **no sink event crosses between runs** — 36 distinct canaries, zero leaks. |
| F | Runs survive a server restart; an exported bundle's manifest hashes all verify; no event in the bundle belongs to another run. |
| G | The kill switch cancels a run in flight. |

## Reading a failure

A `FAIL` line names the case and the mismatch. The per-case JSON is kept under
`$TMPDIR/crosswire-qa/` (`a1.json`, `b1.json`, …) with the full trial list, the
request and reply of every exchange, and the timeline — that is where to look first.

Two failure modes that mean the tool is working, not broken:

- A **bridge action refused with the stop still confirmed** is the safeguard. The
  assertion is on the timeline, not on the action succeeding.
- A **control arm that fires** voids the run as `invalid`. That is the rule doing its
  job; the payload needs fixing, not the rule.

## If the suite hangs or every case errors

Something is already holding a port. `zsh qa/clean.sh` clears
7410, 7500, 7201–7206 and 9910. The suite refuses to start on a dirty machine rather
than producing results that a stray server from an earlier run would corrupt — an
old run's `/api/stop` will cancel a new run's trials.

## What this does not cover

A real model. Every number here comes from the deterministic stand-in, so the suite
proves the **mechanism** — delivery, layers, attribution, verdicts, safeguards,
persistence — and says nothing about how a real model behaves. For that, set
`OPENAI_API_KEY` and run `CROSSWIRE_LIVE=1 go test ./internal/... -run Live`, or
drive a reference target by hand. The measured results in `docs/results/` come from
real-model runs.
