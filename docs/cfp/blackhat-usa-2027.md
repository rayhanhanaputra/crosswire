# Black Hat USA 2027 — submission draft

Two tracks fit. Submit both; they review separately.

- **Arsenal** (tool demo, 1 hour at a station, lower bar, high fit for Crosswire).
- **Briefings** (talk, needs the measured claim; the AI/ML or Application Security track).

Deadlines: check the Black Hat USA 2027 CFP page before writing anything down here.
Black Hat Asia 2027 (Singapore, spring) opens its CFP earlier and is the nearer
Arsenal slot; the same material fits.

Bracketed fields are for the speaker to fill.

---

## Arsenal

**Tool name:** Crosswire

**Short description (≤ 250 characters):**
A single-binary workbench, in the Burp Repeater/Intruder style, for testing AI
agents through the inputs they perceive — camera, microphone, device discovery,
documents — with per-trial canaries, a benign control arm and confidence intervals.

**Long description:**
Prompt-injection tooling tests the chat box. Agents deployed in homes, offices and
call centres act on other inputs: the device list a home platform builds, the audio a
voice assistant hears, the frame a camera agent reads, the invoice an accounts-payable
agent scans. A guardrail placed on the chat path inspects none of them.

Crosswire sends the same instruction through the chat path and through the
perception path, with a benign control of the same length, N times each. Every trial
carries a fresh nonce; the only thing that counts as a hit is a forbidden tool
reaching an out-of-band sink with that nonce in its arguments. Reply text is a hint,
never evidence, which removes the refusal-echo false positive. Verdicts are reported
as k/N with 95 % Wilson intervals; "confirmed" requires repeated hits, a clean
control and non-overlapping intervals.

The channel adapters produce real artifacts: QR and sign PNGs, rendered invoices,
speech WAVs, DTMF tones (and a Goertzel decoder to verify them), Morse pulse
timelines to a serial LED rig. Channels that only relabel text are marked *modeled*
and cannot back a reproducibility claim.

Five reference targets ship in the binary, each a real tool-calling model: a home
assistant that trusts its entity list, a voice assistant with mail and payment tools,
a small-business camera agent, an accounts-payable agent, and an inbox assistant that
opens mail, calendar invites and PDF attachments. Each runs as
`vulnerable`, `hardened` (quarantined perception into a fixed schema, hidden
privileged tools, user-channel confirmation, interlocks in code) or with a chat-path
guardrail. Every run exports a bundle (run JSON, artifacts, sink events, hashes).

**Demo at the station (what the audience sees):**
1. Load the camera reference target; preview a rendered sign as the PNG the vision
   model will read.
2. Send ten injected trials, ten benign controls, ten chat trials with the guardrail
   on. Watch the trial table fill and the verdict panel compute.
3. Show the first-pass result that the control arm invalidated (a tool the agent calls
   on any quiet scene) and the corrected rule.
4. Switch the target to `hardened`, re-run, show 0/10 with the payload still
   delivered, and download the bundle.

**Why it is not another prompt-injection fuzzer:** it measures where the guardrail
is, not what the model says; it has a control arm; it refuses to confirm without one;
and its reference targets differ in architecture rather than in a keyword filter.

**Reference run (2026-10-31, gpt-4o-mini, N = 10 per arm):** with a chat guardrail on,
the same instruction scored 0/10 through chat and 10/10 (home), 9/10 (voice) and 10/10
(camera) through the perception path; hardened targets scored 0/10 on all four with the
nonce delivered.

**Open source:** Apache-2.0 (code), CC BY 4.0 (knowledge base). [repository link]

**Presenters:** [name, affiliation, bio ≤ 100 words]

---

## Briefings

**Title:** The Guardrail Reads the Chat Box. The Agent Reads the Room.

**Abstract (≤ 300 words):**
Agents act on inputs that never pass through a chat interface: the entity list a
home platform assembles from device names, the audio a voice assistant transcribes,
the frames a camera agent describes, the invoices a payments agent extracts.
Prompt-injection defences are placed on the chat path. This talk measures the gap.

We built five reference agents on a production model, each with real privileged tools,
in three configurations: a plain agent loop; the same loop with a prompt-injection
classifier on the chat path; and a hardened design (perception quarantined into a
fixed schema by a tool-less model, privileged tools withheld from the perception path,
confirmation only from the user channel, interlocks in code). We then sent the same
instruction three ways — through the perception channel, as a benign control of equal
length, and typed into chat — ten times each, with a fresh nonce per trial and an
out-of-band sink as the only accepted evidence.

With the classifier on, the instruction scored 0/10 through chat and 10/10, 9/10 and
10/10 through device name, speech and sign. Hardened designs scored 0/10 on every
scenario with the nonce still delivered. The control arm invalidated our own first
camera oracle, which would have reported a 100 % success to a single-send analyzer.
Delivery and compliance separated cleanly: a document channel delivered the payload
9–10/10 and the model complied 2–3/10, while chat complied 10/10. We also report two
channels that cannot carry an instruction at realistic sampling rates.

We release Crosswire, the single-binary workbench used for every number here, with its
reference targets and run bundles, so attendees can measure their own agents the same
way. We position the work against CHAI (physical signs vs. vision agents) and claim
only the measurement, the non-optical channels and the negative results.

**Attendee takeaways:**
- Where a prompt-injection guardrail has to sit to see the inputs an agent acts on.
- A verdict procedure (control arm, nonce, sink, intervals) that stops single-send
  false positives.
- Four architectural changes, with measured effect, that a team can apply this quarter.

**Outline (40 min):** problem and prior work (5) · method (7) · results and the oracle
the control caught (12) · hardened design and what still gets through (6) · tool demo
(6) · limits and Q&A (4).

**Novelty statement:** Not the first physical prompt injection (CHAI, 2025). First, to
our knowledge, to measure guardrail placement across non-optical physical channels with
a control arm and intervals, and to publish channel-capacity negative results alongside
a reusable tool.

**Has this been presented before:** No.

**Speakers:** [name, affiliation, bio]
