# Knowledge Base

The knowledge base is the reference corpus behind Crosswire. It documents
reproducible security techniques, success conditions, detection signals, and defenses
for AI agents receiving text, structured data, sensor, signal, audio, optical,
inter-agent, and RF inputs.

Entries are grouped by the **channel** that carries the injection. Within each
channel folder, the `README.md` is a Summary index of that channel's techniques.
The cross-tabulated Modality Injection Matrix (channel by intent), the machine
readable index, and the corpus statistics are **generated** from entry front matter
by `tools/build_registry.py` and live under [`registry/`](../registry/). Do not
hand-edit the generated files.

## Channels

| Channel | Folder | Scope |
| --- | --- | --- |
| C1 | [C1-sensor-value](C1-sensor-value/) | Spoofed, out-of-range, or overflowing numeric telemetry |
| C2 | [C2-data-field](C2-data-field/) | Instructions hidden in structured fields parsed as text |
| C3 | [C3-actuator-state](C3-actuator-state/) | Binary toggles, Morse, timing patterns the agent observes |
| C4 | [C4-voice-dtmf](C4-voice-dtmf/) | Adversarial speech, DTMF tones, ASR confusion |
| C5 | [C5-optical-visual](C5-optical-visual/) | QR, barcodes, text-in-image, steganography |
| C6 | [C6-rf-protocol](C6-rf-protocol/) | BLE, Zigbee, LoRa, NFC frames |
| C7 | [C7-indirect-inter-agent](C7-indirect-inter-agent/) | Poisoned logs, transcripts, tool outputs, retrieved documents |
| C8 | [C8-feedback-loop](C8-feedback-loop/) | Replay, flooding, timing races on a control loop |

See the [Modality Injection Matrix](../registry/matrix.md) for the channel-by-intent
view with links to every entry, and [corpus statistics](../registry/stats.json) for
counts by channel, intent, and status.

## Entry status

Each entry declares one of two maturity levels:

- `documented` — described with a threat model and a falsifiable success condition.
- `reproduced` — backed by a stored Crosswire run bundle (`bundle:` path under
  `kb/bundles/`) whose verdict is `confirmed` or `observed` through a real, not
  modeled, channel. `tools/build_registry.py --check` fails otherwise.

Until 2026-10-31 every entry claimed `reproduced` while nothing in the repository
could re-run it. All entries were reset to `documented`; status now follows evidence.

## What counts as a physical channel

An entry belongs in the physical corpus when the attacker-controlled content reaches
the agent through something the agent *perceives* — a microphone, a camera, a device
discovery list, a document scan — rather than through a chat message. Entries whose
only difference from chat injection is a JSON key name are kept for completeness
(`webapp`, `inter-agent`, `rag` tags) but are not the project's claim. Two entries
record **channel-capacity negative results** (`capacity-limit` tag): Morse over an
LED and DTMF tones. They exist to show which channels cannot carry an instruction.

## Reference scenarios

Crosswire ships a real tool-calling LLM target for each of these; see
[`../docs/CONTRACT.md`](../docs/CONTRACT.md) §6.

| Entry    | Target          | Channel adapter | Privileged tool            |
| -------- | --------------- | --------------- | -------------------------- |
| CW-0011 | `home-agent`    | `device-name`   | `unlock_door`, `disarm_alarm` |
| CW-0012 | `voice-agent`   | `speech`        | `send_email`, `transfer_funds` |
| CW-0013 | `camera-agent`  | `sign`, `qr`    | `mark_clear`, `disarm_alarm` |
| CW-0014 | `expense-agent` | `invoice`       | `approve_payment`          |

## Adding a technique

Copy [`_template/technique.md`](_template/technique.md), fill in the front matter and
sections, and open a single-file pull request. An entry without a falsifiable
`success` field is not accepted. See [CONTRIBUTING.md](../CONTRIBUTING.md).

## Identifier scheme

Every technique carries two identifiers:

- **slug** — the human-readable filename, for example `C2-I3-smartfarm-note-override`.
- **registry_id** — a stable, never-reused, citable identifier, for example `CW-0001`.

Slugs may be renamed; the registry_id is permanent and is what the paper cites.
