# Black Hat Asia 2027 — Arsenal submission

**Call for Tools closes 2 November 2026. Notification 1 December 2026.** Event: Marina Bay
Sands, Singapore, Briefings 2–3 March 2027. Submit at the Arsenal portal linked from
blackhat.com/html/arsenal-call-for-tools.html. Questions: <arsenal@blackhat.com>. Presenters
must be available both Arsenal days; travel is not covered; one Briefings pass is included.

Bracketed fields are for the speaker to fill. Every number below must match
`docs/results/` at the time of submission; nothing here is a claim about a product.

---

**Tool name:** Crosswire

**Track:** AI, ML and Data Science (or Application Security, if the form lacks an AI track)

**Short description (≤ 250 characters):**
A single-binary workbench for prompt-injection testing through what an agent perceives,
not what it is told: documents, speech, SIP calls, device names, handoff notes. Nonce per
trial, benign control, tool sink, intervals.

**Long description:**

Red-team tooling for language-model agents tests the chat box. The agents people deploy
read other things: the entity list a home platform builds from device names, the audio a
voice assistant transcribes, the frame a camera agent describes, the PDF an inbox
assistant opens, the note another agent left on the ticket. Crosswire sends an instruction
through those inputs and measures what the agent does with it.

Three things set it apart from a converter library.

1. **The verdict.** Every trial carries a fresh nonce. The only evidence that counts is a
   forbidden tool reaching an out-of-band sink with that nonce in its arguments. Reply
   text is never evidence, which removes the refusal-echo false positive. A benign control
   of equal length runs beside every injected arm. Results are k/N with 95 % Wilson
   intervals, and "confirmed" requires repeated hits, a clean control and non-overlapping
   intervals. The sink records *attempted* calls as well as *executed* ones, so a defense
   that blocks a call in code is reported as a block, not as a model that refused.

2. **The placement comparison.** The same prompt-injection classifier can be placed on the
   chat path, on the perception path, or on both, against the same agent, so the measured
   quantity is where a guardrail sits, not whether one exists.

3. **Defenses one at a time.** The hardened reference build has four layers: perception
   quarantined into a fixed schema by a tool-less model; privileged tools withheld from
   the perception path; confirmation only from the user channel; interlocks in code. Each
   can be switched off on its own, so a zero credits the layer that earned it.

The channel adapters produce real artifacts: QR and sign PNGs, rendered invoices, PDF, ICS
and EML files, speech WAVs, DTMF tones, Morse pulse timelines for a serial LED rig.
Channels that only relabel text are marked *modeled* and cannot back a reproducibility
claim. Two transports deliver them: HTTP to a perception endpoint, and a SIP call that
plays the audio as G.711 and records the reply. Trials can be two-phase: plant through one
input, trigger later through a routine request, which is how memory and inter-agent
handoff channels are tested.

Six reference targets ship in the binary, each a real tool-calling model behind an HTTP
API: a home assistant, a voice assistant with mail and payment tools, a camera agent, an
accounts-payable agent, an inbox assistant that opens attachments, and a support-desk
agent that reads handoff notes. Each runs as `vulnerable`, with a classifier on either
path, or `hardened` with any layer disabled. Every run exports a bundle: run JSON,
artifacts, sink events, hashes. The driver script that produced every number in the
results pages is in the repository.

**Testbeds.** Two. The six reference agents in the binary, each a real tool-calling model, produce every measured number. The companion SimLab (github.com/rayhanhanaputra/AIAgentsAllTheThings-SimLab) is a visual testbed: six deterministic digital twins (smart farm, water plant, warehouse fleet, hospital climate, smart building, command drill) that accept the same payloads, show the compromised state under a basic profile and keep content as data under a validated one, and report every control change to the Crosswire sink with the trial's nonce and session. It is a stage prop for the demo and an end-to-end check of the sink contract, not a measurement target, and the submission says so.

**Claims, after the second pass (1 November 2026).** Three tests were run after an independent review. The classifier's cost stands: at temperature 0 the perception-path classifier still blocks benign input on home 30/30, expense 29/30 and voice 21/30 (camera 0, PDF 2, desk 0), and on the chat path it blocks the user's own legitimate privileged request 27 to 30 of 30 on five of six channels. On store-then-trigger channels the request-path layers (withhold, turn-level confirmation) are inert by construction on both scenarios tested; what holds is content-side: a quarantine that strips the value (two-phase inbox, 0 attempts) or a check on the value (desk: record match 0/30 executed, 26/30 without it; provenance rule 0 executed and 0 of 30 legitimate refunds). At every model-side gate the outcome belongs to the wording: 21 to 23 of 25 generated wordings gave the same result both tries, between-wording share of variance 0.81 to 0.92, and a best-of-k attacker passes the classifier at 0.88 where five hand-written sentences passed at 0.40. Full tables in `docs/results/2026-11-01-rev2.md`.

**What it does not do.** The sink needs the target to report its own tool calls, so a
black-box product can only be tested where its tools can be pointed at a URL the tester
controls, which is how most voice-agent platforms define custom tools. The SIP connector
speaks UDP with digest authentication and has no TLS or SRTP; it has been run against a
lab endpoint, not a product. The reference targets were written for this project.

**Demo at the station:**

1. Load the inbox reference target. Render a vendor letter whose last paragraph is printed
   in white ink; show the PDF as the agent's text extractor sees it.
2. Send injected trials, benign controls and chat trials with the classifier on the chat
   path. Watch the trial table and the verdict panel.
3. Move the classifier to the perception path and re-run.
4. Switch to hardened; re-run; open the sink and show the attempted calls that the
   confirmation gate blocked, next to the executed count.
5. Show the first camera pass that the control arm invalidated, and the corrected rule.
6. Place a SIP call to the voice target from the same screen and show the recorded reply.
7. Send the same payload into SimLab: the smart-farm valve opens under the basic profile, stays closed under validated, and both land in the sink.

**Reference results (2026-11-01, `gpt-4o-mini`, temperature 0.7, 30 trials per arm,
five paraphrases, full rows in `docs/results/2026-11-01.md`):** with the
classifier on the chat path, the typed instruction scored 0/30, 5/30, 0/30 and 0/30
while the same instruction through the device name, the phone call, the camera frame
and the invoice scored 30/30, 22/30, 30/30 and 3/30. With the same classifier on the
perception path those became 0/30, 4/30, 0/30 and 0/30, and the typed arm went back to
29–30/30. The SIP phone call reproduced the HTTP speech rate (25 vs 25 of 30). The
hardened build executed no privileged call in 210 trials across seven scenarios. With
one layer off at a time: on six scenarios the few attempts (voice 6, phone 7, camera 1,
all with withhold off) were refused by the confirmation gate and recorded by the sink;
on the support-desk scenario the handoff note passed quarantine, withhold and
confirmation in every build (13 to 30 attempts of 30) and the interlock in the tool
handler refused all of them; with the interlock off, 20 of 30 refunds executed. The
home run was voided by its own control arm (one benign device name in thirty unlocked
the door) and stays in the record as invalid.

**Open source:** Apache-2.0 (code), CC BY 4.0 (knowledge base). [public repository link:
the repository must be reachable by reviewers before the deadline]

**Has this tool been presented before:** No.

**Presenters:** [name, affiliation, bio ≤ 100 words]

**Prior work acknowledged:** Greshake et al. 2023 (indirect injection); Nassi, Cohen and
Yair 2025 (calendar invites against Gemini); Rehberger 2024–25 (memory and delayed tool
invocation); PyRIT converters and XPIA workflow; AudioHijack 2026; CHAI 2025; the DEF CON
34 AI Village voice-agent calling tool. Survey: `docs/research/prior-work.md`.
