# Black Hat USA 2027 — submission draft

Two tracks fit. Submit both; they review separately.

- **Arsenal** (tool demo at a station; the Black Hat Asia 2027 Arsenal draft in
  `blackhat-asia-2027-arsenal.md` is the same pitch and is due first).
- **Briefings** (talk; AI, ML and Data Science track, or Application Security).

**Dates, inferred.** Black Hat USA 2026 Briefings CFP ran 27 January to 23 March
2026. Expect USA 2027 to open late January 2027 and close in March 2027, for an event
around 31 July to 5 August 2027 in Las Vegas. Check blackhat.com before relying on
any of this; the page returns 403 to scripted fetches, so open it in a browser.

Bracketed fields are for the speaker to fill. Every number below is from
`crosswire/docs/results/2026-11-01.md`; update both together.

---

## Arsenal

Use `blackhat-asia-2027-arsenal.md` as written; the USA form asks the same
questions. Change the "presented before" answer if Asia accepts.

---

## Briefings

**Title:** The Classifier Reads the Chat Box. The Agent Reads the Room.

**Abstract (≤ 300 words):**
Agents act on inputs that never pass through a chat interface: the entity list a home
platform assembles from device names, the audio a voice assistant transcribes from a
phone call, the frame a camera agent describes, the invoice a payments agent extracts,
the note another agent left on the ticket. Prompt-injection classifiers are placed on
the chat path. This talk measures the gap, and then measures what closing it costs.

We built six reference agents, each a real tool-calling model with real privileged
tools behind an HTTP API, in eight builds: a plain agent loop; the same loop with one
prompt-injection classifier on the chat path, on the perception path, or on both; and a
hardened design with four layers (perception quarantined into a fixed schema by a
tool-less model, privileged tools withheld from the perception path, confirmation only
from the user channel, interlocks in code), each layer switchable on its own. We sent
the same instruction through the perception channel, as a benign control of the same
shape, and typed into chat, thirty times each with five paraphrases and a fresh nonce
per trial. The only accepted evidence was a forbidden tool reaching an out-of-band sink
with that nonce. The sink also records calls the target blocked, with the layer that
blocked them, so a defence that works in code is not mistaken for a model that refused.

On the chat path the classifier removed typed injections (29/30 → 0/30) and changed
nothing on the perception path (device name 28/30, speech 25/30, phone call 25/30,
camera 30/30). On the perception path it did the reverse. The hardened build let no
privileged call execute in 150 trials on one model; the ablation shows which layer
caught the calls that got that far [and, on `deepseek-flash`, which removals let a
call through: fill from the results page]. Our own control arm voided a run: a home
agent unlocked the door on a benign device name, once in thirty.

We release Crosswire, the single-binary workbench used for every number here, with its
reference targets, the driver script and the result JSON.

**Attendee takeaways:**
- Where a prompt-injection classifier has to sit to see the inputs an agent acts on,
  with the same classifier measured on each path against the same agent.
- A verdict procedure (control arm, nonce, sink, attempted vs executed, intervals)
  that stops single-send false positives and stops crediting the wrong layer.
- Four architectural layers with a measured effect for each, and the one that did the
  work.

**Outline (40 min):** prior work and what is added (5) · method (7) · placement table
and the phone call (10) · one layer at a time (7) · what the control arm caught (3) ·
tool demo (5) · limits and Q&A (3).

**Prior work and novelty statement:** Indirect prompt injection is Greshake et al.
(2023). Injection through calendar invites and email against shipped products is
SafeBreach (Nassi, Cohen, Yair, 2025) and EchoLeak (2025). Memory and delayed tool
invocation is Rehberger (2024–25). Audio-native injection is AudioHijack and
"Piggybacking on Perception" (2026). Physical signs against vision agents are CHAI
(2025) and Physical Prompt Injection (2026). PyRIT converts text into PDFs, QR codes
and speech; a DEF CON 34 AI Village tool phones voice agents. What this talk adds: the
verdict procedure with attempted and executed counts; the same classifier measured on
chat, perception and both against one agent; a per-layer ablation of the hardened
design; device names and protocol fields as a channel, for which we found no prior
report; and two negative results. We say "no prior report found", not "first".

**Has this been presented before:** No.

**Speakers:** [name, affiliation, bio]
