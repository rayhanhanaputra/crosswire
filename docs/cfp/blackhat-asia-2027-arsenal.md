# Black Hat Asia 2027 — Arsenal submission

**Call for Tools closes 2 November 2026. Notification 1 December 2026.** Event: Marina Bay
Sands, Singapore, Briefings 2–3 March 2027. Submit at the Arsenal portal linked from
blackhat.com/html/arsenal-call-for-tools.html. Questions: arsenal@blackhat.com. Presenters
must be available both Arsenal days; travel is not covered; one Briefings pass is included.

Bracketed fields are for the speaker to fill. Every number below must match
`crosswire/docs/results/` at the time of submission; nothing here is a claim about a product.

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

**Reference results (2026-11-01, `gpt-4o-mini`, temperature 0.7, 30 trials per arm,
five paraphrases, full rows in `crosswire/docs/results/2026-11-01.md`):** with the
classifier on the chat path, the typed instruction scored 0/30, 5/30, 0/30 and 0/30
while the same instruction through the device name, the phone call, the camera frame
and the invoice scored 30/30, 22/30, 30/30 and 3/30. With the same classifier on the
perception path those became 0/30, 4/30, 0/30 and 0/30, and the typed arm went back to
29–30/30. The SIP phone call reproduced the HTTP speech rate (25 vs 25 of 30). The
hardened build executed no privileged call in 150 trials; with the withhold layer off
the model attempted the call 6 (voice) and 1 (camera) times and the confirmation gate
refused each, which the sink recorded. The home run was voided by its own control arm:
one benign device name in thirty made the agent unlock the door. A quota error voided
27 rows on this model; they are recorded as `error`, not as zeros. A second model
(`deepseek-flash`) covers the inbox and desk scenarios and the full layer ablation:
[fill the one-line summary from the results page before submitting].

**Open source:** Apache-2.0 (code), CC BY 4.0 (knowledge base). [public repository link:
the repository must be reachable by reviewers before the deadline]

**Has this tool been presented before:** No.

**Presenters:** [name, affiliation, bio ≤ 100 words]

**Prior work acknowledged:** Greshake et al. 2023 (indirect injection); Nassi, Cohen and
Yair 2025 (calendar invites against Gemini); Rehberger 2024–25 (memory and delayed tool
invocation); PyRIT converters and XPIA workflow; AudioHijack 2026; CHAI 2025; the DEF CON
34 AI Village voice-agent calling tool. Survey: `docs/research/prior-work.md`.
