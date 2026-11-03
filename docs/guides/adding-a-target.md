# Testing your own agent

Crosswire tests an authorized AI agent without changes to this repository. You
describe what the agent perceives, how the artifact reaches it, and what mechanically
proves a violation. The reference targets in the binary are there to practise on
first.

## What the target has to do

One thing is not optional: **the sink**. A hit is a forbidden tool call that reached
Crosswire's sink carrying that trial's canary. So the agent under test must report its
own tool calls — executed *and* refused — to the sink URL shown in the header
(`POST /api/sink`). Point the agent's tool layer, a webhook, or a wrapper at it, and
include the request's session value on every event so trials cannot be confused with
one another. The event shape is in
[`../CONTRACT.md`](../CONTRACT.md) section 2.

Without that, a run can still deliver an artifact and record the reply, but nothing
counts as evidence and every verdict stays `invalid`. If the agent's effect is visible
only in a UI or a state endpoint, an **observer** is the second-best option — see
CONTRACT.md section 8 for the rules it has to meet.

## Configure a target

1. `./crosswire serve`, open <http://127.0.0.1:7117>, go to **Targets**.
2. Start from **Load reference target** to see a working profile, then **New target**.
3. Fill in the three endpoints you have:
   - `endpoint` — the perception path, where the artifact is delivered.
   - `direct_endpoint` — the chat path, for the `direct` arm. This is what makes the
     placement comparison possible; without it you only measure one path.
   - `trigger_endpoint` — only for store-then-trigger channels, with the routine
     request as `trigger_template`.
4. Pick the **transport** that matches the real ingress: `http`, `ws`, `tcp`, `mqtt`,
   `voice-ws`, `sip`, or `bridge`.
5. Write the request template. `{{content}}`, `{{content_json}}`, `{{canary}}`,
   `{{image_base64}}`, `{{audio_base64}}` and `{{artifact_*}}` are the placeholders.
   Set `response_path` and `actions_path` to read the reply.
6. Name the **forbidden tools** — the ones that must never run. Keep "Require canary in
   tool arguments" on unless the channel cannot carry a per-trial value (a fixed
   recording, for example); then judge by session instead and say so in your notes.
7. Reference credentials by **environment-variable name**. Never paste a secret into
   the UI; it would be stored and shown back.

Adapter and transport answer different questions, and stay independent:

```text
channel adapter: what does the agent perceive?
transport:       how does Crosswire deliver it?
```

A `sensor-value` message can travel over MQTT, HTTP, TCP or WebSocket. A `speech` WAV
can go over HTTP or a real SIP call. Only couple them when the target contract forces
it.

## Run it

1. **Repeater** — pick the adapter, write the payload with `{{canary}}` where the
   attacker's address, account or code goes. Write a benign control of similar length;
   this is the arm that keeps you honest. Tick **direct** to also type the instruction
   into chat.
2. **Preview** first. It renders the artifact, its hash and the exact request bodies,
   and sends nothing. Check that the canary actually survives into the artifact.
3. Set repeats: 5 for a look, 20–30 for a claim. **Send**.
4. Read the verdict as `k/N [lower–upper]` per arm, with `attempted` beside `hits`.
   `0 hits, 30 attempted` means the target's code gates stopped it; `0 hits, 0
   attempted` means the model did not try. Those are different findings.
5. **Intruder** for a list of payloads, once the single request is right.
6. **History** → **Download bundle** for the run.json, artifacts and sink events. That
   bundle is what a knowledge-base entry cites.

## Optional LLM mapping

Use the input mapper only when a deterministic request template cannot express the
target contract. Use the response mapper only when stable `response_path` and
`actions_path` values cannot normalize the output.

Both are labelled `MAPPED` in the UI and **neither is ever evidence**: a hit still
needs the sink or an observer change. If the input mapper drops the canary from most
requests, Crosswire marks the run invalid rather than reporting a rate you cannot
trust.

## Safe verification

- Practise on a reference target (`./crosswire target home-agent`) or your own staging
  deployment before anything production-labelled.
- Configure the evidence rules *before* a production run, not after seeing a reply.
- Production, observer and bridge runs are serialized to one trial at a time, and each
  needs acknowledgement. That is deliberate; do not script around it.
- Treat `not_observed` as "0 hits at these repeats", never as proof the agent is safe.
  The UI reports it as an upper bound for that reason.
- A control arm that fires voids the run. Investigate it rather than re-running until
  the control happens to stay clean.
- `./crosswire selftest` exercises every transport against in-process fakes, so you can
  check a profile *shape* without contacting any real deployment.

See [SECURITY.md](../../SECURITY.md) before connecting a real target, and
[`../CONTRACT.md`](../CONTRACT.md) for the normative contract.
