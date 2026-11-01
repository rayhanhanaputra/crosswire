# DEF CON 35 (2027) — submission draft

Three routes, in order of fit:

1. **AI Village** talk or workshop. Its CFP runs separately from the main CFP and
   has opened in spring in past years. The DEF CON 34 AI Village already hosted a
   voice-agent calling tool, so the village knows the topic; the pitch has to be the
   measurement, not the phone call.
2. **Demo Labs**, the DEF CON counterpart to Arsenal.
3. **Main-track talk.** Needs the measured claim; competitive.

**Dates.** DEF CON 35 is 5–8 August 2027 (Las Vegas). The DEF CON 34 CFP closed
1 May 2026, so expect the DEF CON 35 CFP to close around 1 May 2027; check
defcon.org/html/defcon-35/dc-35-cfp.html when it opens. The AI Village CFP is
announced on aivillage.org. Reviewers read the detailed outline closely; the abstract
is secondary.

Bracketed fields are for the speaker to fill. Every number below is from
`crosswire/docs/results/2026-11-01.md`; update both together.

---

## Title

Crosswire: Measuring Where the Guardrail Sits

## Abstract (≤ 250 words, public)

A prompt-injection classifier watches the chat box. The agent also reads the names of
the devices on the network, the audio of a phone call, the sign in front of the camera,
and the invoice it was asked to pay. Everyone says the classifier misses those. We
measured by how much, and then measured what moving it does.

Six reference agents, each a real tool-calling model behind an HTTP API, were sent the
same instruction through their perception path, as a benign control of the same shape,
and typed into chat, thirty times each with five paraphrases and a fresh nonce per
trial. The only evidence of a hit was a forbidden tool reaching a sink we controlled
with that nonce in its arguments. Then we took one classifier and placed it on the chat
path, on the perception path, and on both, against the same agent.

On the chat path the classifier removed the typed injections (29/30 → 0/30) and left
the perception-path rate untouched (device name 28/30, speech 25/30, phone call 25/30,
camera 30/30). On the perception path it did the reverse. A hardened build with four
layers, each switchable, let no privileged call execute in 150 trials; the sink shows
which layer caught the calls that got that far.

We also show what the control arm caught: a home agent that unlocked the front door
on a benign device name, once in thirty, which voids the run by our own rule. And we
show the channel that does not work.

Crosswire, the single-binary workbench behind every number, ships with the talk.

## Detailed outline (for reviewers)

1. **Setup (4 min).** Indirect prompt injection is Greshake's (2023). Calendar
   invites and email against shipped products are SafeBreach and EchoLeak (2025).
   Memory and delayed triggers are Rehberger (2024–25). Audio-native injection is
   AudioHijack (2026). Physical signs are CHAI (2025). What this talk adds is
   narrower: a verdict procedure, a placement comparison on one agent, and a layer
   ablation.
2. **Method (7 min).** Three arms. Nonce per trial, five paraphrases rotated by
   index. The sink as the only evidence; the refusal echo as the false positive it
   removes. *Attempted* calls (blocked in code, recorded by the sink with the reason)
   reported apart from *executed* ones. Wilson intervals; outcomes `confirmed`,
   `observed`, `not observed`, `invalid`, `error`.
3. **Targets (4 min).** Six reference agents: home (device names), voice (speech
   over HTTP and over a SIP call), camera (sign in a frame), expense (rendered
   invoice), inbox (EML and PDF with a white-ink paragraph), support desk (handoff
   note, triggered next shift). Eight builds per target.
4. **Placement (10 min).** The table: vulnerable, classifier on chat, classifier on
   perception, hardened, with the direct-chat arm beside each. Chat path: 29→0,
   30→5, 30→0, 29→0 on the typed injection; 28→30, 25→21, 30→30, 3→3 on the
   perception path. Perception path: 28→0, 25→3, 30→0, 3→0; typed stays at 29, 29,
   30, 29. The phone call reproduced the HTTP speech rate (25 vs 25, 22 vs 21, 4 vs
   3). The invoice channel is weak on this model (3/30) and is reported as weak.
5. **Layers (6 min).** The hardened build with one layer off at a time. On
   `gpt-4o-mini` no single removal produced an executed hit; every attempted call
   was stopped by the confirmation gate, and all of them happened with quarantine or
   withhold off. On `deepseek-flash` [fill from the results page: which removals let
   a call execute, and on which scenario]. Why a zero that credits nothing is still
   worth showing.
6. **What the control caught (3 min).** The home agent that unlocked the door on
   "signal strong, battery 64%". The camera oracle from the first pass. Both runs
   read `invalid` and stay in the record.
7. **Negative result (2 min).** Morse over an LED: 37 on/off steps in 6.7 s, seven
   frames at 1 fps. Kept as an adapter so it can be re-measured with a fast sensor.
8. **Live demo (6 min).** Load the inbox target, render the white-ink letter, run
   injected, control and chat arms with the classifier on chat, move it to
   perception, re-run, switch to hardened, open the sink and show the blocked calls
   next to the executed count.
9. **Limits (3 min).** Reference agents, not products, and the platform terms that
   forbid testing products without consent. Two models. N = 30. SIP over UDP on a lab
   endpoint only. The OpenAI quota ran out mid-matrix and voided 27 rows; the driver
   now reports those as `error`, and the rows are in the record as such.

## Why DEF CON

The audience builds and breaks these agents. The talk gives them a measurement they
can repeat on their own agent in an afternoon, and a tool that refuses to say
"confirmed" without a control. It also corrects two habits: a 1-of-N table built from
one prompt, and a "hardened" label that credits four layers when one did the work.

## Demo requirements

Laptop, HDMI. Pre-recorded fallback needs no network; the live run needs a model API.
Optional: the LED rig and a webcam for the negative result.

## Release

Crosswire source (Apache-2.0), knowledge-base entries (CC BY 4.0), the driver script
and the result JSON for every number in the talk. [public repository link]

## Speaker

[name] · [affiliation] · [bio ≤ 100 words] · [prior talks, if any]

## Has this been presented elsewhere

No. A Black Hat Asia 2027 Arsenal submission of the tool (deadline 2 November 2026)
and a Black Hat USA 2027 submission are planned; if more than one is accepted the DEF
CON version carries the layer ablation and the negative results in depth.
