# Glossary

Canonical definitions used across Crosswire and the knowledge base. Other documents
reference this file rather than redefining terms.

- **Agent** — an LLM-driven system that autonomously observes inputs, reasons, and
  invokes tools or actuators, including physical ones.
- **Channel** — the medium through which the agent receives an input: an MQTT topic, a
  phone line, a microphone, a camera, a serial port, an RF interface, or a retrieved
  document.
- **Channel adapter** — the Crosswire component that turns payload fields into the
  artifact the target perceives: a PNG, a WAV, a PDF, a pulse timeline, an NDEF
  message, or a JSON message. An adapter that only relabels text is **modeled**.
- **Artifact** — the rendered bytes an adapter produces, hashed and stored with the
  trial. A real artifact is one a reviewer can open and decode back.
- **Transport** — the delivery mechanism between Crosswire and the target: HTTP,
  WebSocket, TCP, MQTT, realtime voice over WebSocket, SIP, or a guarded device
  bridge.
- **Target profile** — a saved, non-secret description of an authorized agent endpoint,
  its transport, request template, safety environment, and evidence rules. Credentials
  appear only as the names of environment variables.
- **Input mapper** — an optional OpenAI-compatible model that converts an encoded input
  into a target-specific request shape when a deterministic request template is not
  sufficient. Its output remains untrusted and is validated before delivery.
- **Repeater** — the single-payload tool: choose an adapter, preview the artifact and
  the exact request, then run repeats of the injected, control and direct arms.
- **Intruder** — the same, over a list of payloads, with one verdict per payload.
- **Sink** — the out-of-band receiver the target posts its executed and refused tool
  calls to. It is where evidence comes from.
- **Canary** — the fresh per-trial nonce rendered into the payload. A hit requires it
  in the forbidden tool's arguments, which is what attributes evidence to one trial.
- **Observer** — an optional second evidence source for an agent whose effect shows
  only in a UI or a state endpoint, read before and after each trial. A change counts
  only under the sink's rules: the trial's nonce where the page can show one, the
  control arm checked too, and the run voided if the control changes.
- **Hit** — a forbidden tool that *executed* at the sink with that trial's canary.
- **Attempted** — a forbidden call the target refused in code (a confirmation gate, an
  interlock, a withheld tool). Counted and reported, but never a hit.
- **Arm** — one condition repeated N times: `injected`, the benign `control`, `direct`
  chat, `benign_direct`, or `utility`.
- **Verdict** — the outcome over the arms: `confirmed`, `observed`, `not_observed`,
  `invalid`, or `error`. `confirmed` needs repeated hits, a clean control and
  separated Wilson intervals.
- **Run bundle** — the exported zip (run.json, summary.json, artifacts, sink events)
  that a knowledge-base entry must cite to claim `reproduced`.
- **Signal-to-prompt injection** — an attack that uses a non-text channel to place
  attacker-controlled content into the agent's reasoning context. Also called
  cross-channel injection.
- **Trust boundary** — the line between data the agent should treat as untrusted
  observation and content it treats as trusted instruction or authoritative fact.
- **Privileged action** — a tool call or actuator command with real-world effect,
  especially one governed by a safety policy.
- **Oracle** — the mechanism that decides whether an attack succeeded, as a predicate
  over the target's action log.
- **Action log** — the structured record of every tool call and actuator command the
  instrumented target emits, with arguments and the id of the triggering input.
- **Benign baseline (benign twin)** — a legitimate input with the same surface
  features as an attack but no injected instruction, which must not trigger the unsafe
  action; used to measure false positives.
- **Attack Success Rate (ASR)** — the fraction of malicious runs the oracle scores as
  successful.
- **False-Positive Rate (FPR)** — the fraction of benign runs that nonetheless produce
  a policy-violating action.
- **Staging environment** — an explicitly configured lab or pre-production target used
  for controlled assessment before production.
- **Production environment** — an explicitly configured, authorized real target.
  Every run requires acknowledgement; campaigns are forced to concurrency one.
