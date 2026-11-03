# Threat Taxonomy and Modality Injection Matrix

This document defines how attacks against AI agents integrated with IoT and
cyber-physical systems are classified in Crosswire. It is intended to be
stable enough to organize input adapters, security tests, and the knowledge base.

## Definitions

Terms used throughout the project (agent, channel, signal-to-prompt injection, trust
boundary, privileged action, oracle, action log, benign baseline) are defined once in
the canonical [glossary](glossary.md). This document uses them as defined there.

## Classification structure

Every technique is described along three axes. The axes are not fully independent:
intents co-occur (I1 instruction-override usually accompanies I2 unauthorized-action),
so an entry may declare several. For matrix and statistics purposes the **first** intent
listed is the primary one; the rest are mechanism or secondary-effect flags.

### Axis 1 — Channel (how the injection enters)

| ID | Channel class | Examples |
| --- | --- | --- |
| C1 | Sensor-value injection | Spoofed, out-of-range, or overflowing numeric telemetry |
| C2 | Data-field injection | Instructions hidden in structured fields parsed as text (JSON labels, device names, tags, metadata) |
| C3 | Actuator / state-channel injection | Binary toggle sequences, Morse via relay/LED/buzzer, timing patterns observed by the agent |
| C4 | Acoustic / voice injection | Adversarial or synthesized speech, DTMF tones, ultrasonic carriers, ASR homophone/homoglyph confusion |
| C5 | Optical / visual injection | QR and barcodes, text-in-image, image steganography, adversarial patches |
| C6 | RF / protocol injection | BLE advertisements, Zigbee, LoRa, NFC frames carrying instructions or spoofed telemetry |
| C7 | Indirect / inter-agent injection | Poisoned logs, transcripts, tool outputs, retrieved documents that another agent later ingests |
| C8 | Feedback-loop / control manipulation | Replay, flooding, and timing races that manipulate a control loop without instruction injection |

**Channel decision rule.** The axis is not a single dimension — it mixes the physical
medium (C4 acoustic, C5 optical, C6 RF), the content encoding (C1 numeric, C2 structured
field, C3 state pattern), provenance (C7 inter-agent), and temporal composition (C8). To
classify a technique unambiguously, assign the channel by **the interface class of the
agent's perception path that decodes the attack** (what the agent reads it as), and
record the medium and encoding as tags. So a BLE advertisement carrying an instruction
is C6 (the agent decodes an RF frame) tagged `ble`; a spoofed numeric value carried over
RF is C1 (the agent decodes a number); a flood of readings is C8 (the agent's decision
is manipulated over a window, not by one message). Where a technique spans two channels,
the entry's channel is the primary one and the other is noted (for example, CW-0001 is
C2 with a C1 value-injection variant).

### Axis 2 — Adversary intent (what the attacker wants)

| ID | Intent | Description |
| --- | --- | --- |
| I1 | Instruction override / goal hijacking | Redirect the agent's objective |
| I2 | Unauthorized tool / actuator invocation | Cause an action outside the agent's remit |
| I3 | Safety-limit violation | Drive a permitted action past a hard safety bound |
| I4 | Data exfiltration | Extract secrets, credentials, PII, or PHI |
| I5 | Denial of service / resource abuse | Exhaust actuators, budgets, or attention |
| I6 | Persistence / state corruption | Leave the system in a lasting unsafe state |

### Axis 3 — Effect location (where the impact lands)

Digital-only (data leaves the system) versus cyber-physical (an actuator or physical
process is affected). Crosswire supports both because either can be reached through
the same encode, deliver, capture, and evaluate workflow.

## Modality Injection Matrix

The matrix below is illustrative: each cell names a representative technique shape for
that channel and intent, to convey the space the project covers. The authoritative,
entry-linked matrix that reflects the actual corpus is generated at
[`registry/matrix.md`](../registry/matrix.md); empty cells there are open contribution
targets rather than claims of coverage. All eight channels (C1–C8) and all six intents
(I1–I6) appear below.

| Channel \ Intent | I1 Override | I2 Tool abuse | I3 Safety-limit | I4 Exfiltration | I5 DoS | I6 Persistence |
| --- | --- | --- | --- | --- | --- | --- |
| C1 Sensor value | Out-of-range value forces emergency branch | Value crosses a control threshold | Reading drives action past a safety bound | Value used as a covert readback | Flood of readings skews averaging | Persisted false calibration baseline |
| C2 Data field | Instruction in a JSON label field | Field text names a forbidden tool | "Override the limit" in a device note | Field text requests a stored secret | Oversized field exhausts context | Stored note reasserted each cycle |
| C3 Actuator state | Toggle pattern read as a command | Morse over relay names a tool | Timed pattern requests extended run | State channel exfiltrates a bit stream | Rapid toggling overloads the loop | Latched actuator state persists |
| C4 Voice / DTMF | Spoken "ignore your instructions" | Tone sequence mapped to an action | Spoken request to bypass a limit | Voice request for caller data | Tone flooding of the line | Persistent IVR/session state poisoned |
| C5 Optical / QR | Text-in-image instruction | QR encodes a tool call | Image requests a safety override | QR requests data disclosure | Adversarial patch stalls perception | Physical QR left in the environment |
| C6 RF / protocol | Beacon name carries an instruction | Frame triggers a tool | Spoofed telemetry defeats a limit | Covert channel exfiltrates data | Frame flooding | Rogue paired device persists |
| C7 Indirect / inter-agent | Poisoned transcript redirects a peer | Log entry induces a tool call | Poisoned doc removes a constraint | Chained leak across agents | Poison amplified across a fleet | Poisoned long-term memory or vector store |
| C8 Feedback-loop | Oscillation steers the setpoint | Timing race forces a redundant actuation | Replayed readings defeat a rate limit | Timing side channel leaks state | Flooding destabilizes the loop | Sustained bias locks a corrupted setpoint |

## Mapping to standard frameworks

Each knowledge-base entry records its mapping to four frameworks, via the controlled
vocabularies under [`mappings/`](../mappings/):

- **OWASP Top 10 for LLM Applications (2025 edition)** — Prompt Injection (LLM01),
  Sensitive Information Disclosure (LLM02), Improper Output Handling (LLM05, for
  actuator commands consumed without validation), Excessive Agency (LLM06), and Vector
  and Embedding Weaknesses (LLM08, for RAG). Ids are pinned to the 2025 edition; the
  2026 edition renumbers several of them.
- **OWASP Agentic AI — Threats and Mitigations (T1–T17)** — Tool Misuse (T02), Intent
  Breaking and Goal Manipulation (T06), Privilege Compromise (T03), Resource Overload
  (T04), Identity Spoofing (T09), Agent Communication Poisoning (T12), Memory Poisoning
  (T01).
- **MITRE ATLAS** — LLM Prompt Injection (AML.T0051, Direct/Indirect sub-techniques)
  and RAG Poisoning (AML.T0070).
- **MITRE ATT&CK for ICS** — for the cyber-physical control channels (C1, C2, C3, C8),
  which ATLAS does not cover: Unauthorized Message (T1692), Manipulation of Control
  (T0831), Denial of Service (T0814), Loss of Safety (T0880).

Mappings are recorded per entry rather than asserted in bulk, so coverage claims stay
honest as the corpus grows. `unsafe-actuation` is an effect, so it is a tag, not a
framework mapping.

## Severity rubric

Each entry declares a severity. It is assigned from the effect and its reversibility,
not the ease of exploitation:

- **critical** — an irreversible or safety-critical physical effect (a door unlocked, an
  alarm disarmed, a gate opened).
- **high** — a bounded but real physical or financial effect (a valve or heater driven
  past a safety cap, a refund or shipment released, a card number disclosed).
- **medium** — a policy violation with limited or easily-reversed effect.
- **low** — a violation with negligible effect, of interest mainly as a precursor.

## Out of scope

This project covers signal-to-prompt injection: attacks that reach an agent through a
channel it perceives and drive it to an unsafe action. Several agentic threats that
frameworks catalogue are deliberately **out of scope** here, because they are not
channel-injection or are covered better elsewhere:

- model extraction, model/data poisoning at training time, and supply-chain compromise
  of models or dependencies (OWASP LLM03/LLM04:2025; ATLAS ML-development tactics);
- unexpected code execution and sandbox escape (OWASP Agentic T11);
- overwhelming or socially engineering a human in the loop (T10, T15).

Runtime long-term-memory poisoning is in scope when attacker-controlled content enters
through a channel the agent later retrieves; see CW-0010.

These are noted so the omissions read as scoping decisions, not oversights, and are
candidates for future entries or explicit companions.

## Technique record format

Each knowledge-base file begins with YAML front matter so entries are machine indexable
and linkable from a Crosswire run bundle. The canonical template is
[`kb/_template/technique.md`](../kb/_template/technique.md); the fields are:

```yaml
---
id:          Cx-Iy-scenario-shortname   # slug; equals the filename
registry_id: CW-XXXX                   # stable, citable; assigned on merge
title:       Human-readable title
channel:     Cx                         # C1..C8
intent:      [Iy]                        # one or more of I1..I6
effect:      cyber-physical              # cyber-physical | digital
severity:    medium                      # low | medium | high | critical
scenario:    Deployment context
preconditions: Attacker position and required access
success: >
  Falsifiable condition, as a predicate over a privileged action in the action log.
mappings:                                # at least one non-empty mapping required
  owasp-llm:        []                    # ids from mappings/owasp-llm.yaml (2025 edition)
  owasp-agentic:    []                    # ids from mappings/owasp-agentic.yaml
  mitre-atlas:      []                    # ids from mappings/mitre-atlas.yaml
  mitre-attack-ics: []                    # ids from mappings/mitre-attack-ics.yaml
tags:        []                          # ids from mappings/tags.yaml
status:      documented                  # documented | reproduced
added:       YYYY-MM-DD
updated:     YYYY-MM-DD
references:
  - "Author, Title, URL, date"
---
```

The `success` field is mandatory. An entry without a falsifiable success condition is
a description, not a technique, and is not accepted into the corpus.
`tools/build_registry.py` validates the front matter and checks that `mappings` and
`tags` identifiers exist in `mappings/`.
