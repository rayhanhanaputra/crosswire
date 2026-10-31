# DEF CON 35 (2027) — submission draft

Three routes, in order of fit:

1. **AI Village** talk or workshop — the natural home; its CFP runs separately from
   the main CFP and usually opens in spring.
2. **Demo Labs** — hands-on tool slot, the DEF CON counterpart to Arsenal.
3. **Main-track talk** — needs the measured claim; competitive.

Deadlines: check the DEF CON 35 CFP and the AI Village CFP pages before relying on a
date. DEF CON reviewers read the detailed outline closely; the abstract is secondary.

Bracketed fields are for the speaker to fill.

---

## Title

Crosswire: What Your Agent Hears That Your Guardrail Never Sees

## Abstract (≤ 250 words, public)

Your prompt-injection filter watches the chat box. Your agent also reads the names of
the devices on your network, the words a television says in the next room, the sign in
front of the camera, and the notes on a supplier's invoice. We measured how much of
that the filter sees. The answer is none of it, and the agent acts on it anyway.

We built four reference agents on a production model — a home assistant, a voice
assistant with mail and payment tools, a small-business camera agent, an
accounts-payable agent — and sent the same instruction three ways, ten times each:
through the physical channel, as a benign control, and typed into chat with a guardrail
on. Hits were counted only when a forbidden tool reached a sink we controlled with that
trial's nonce. Through chat: 0/10. Through the device list, the microphone and the
camera: 10/10, 9/10, 10/10. A hardened design (perception quarantined into a schema,
privileged tools withheld, user-channel confirmation, interlocks in code) scored 0/10
everywhere with the payload still delivered.

We also show the channels that do not work — Morse on an LED at camera frame rates,
DTMF as a language carrier — because negative results are what stop the next
over-claim. And we show the oracle our own control arm invalidated.

Everything runs from one binary, Crosswire, released with the talk: a Burp-style
Repeater and Intruder for perception paths, with run bundles anyone can re-check.

## Detailed outline (for reviewers)

1. **Setup (4 min).** The representation gap in one sentence. Where products put
   the guardrail and why. CHAI and prior physical injection work; what we do and do
   not claim.
2. **Method (7 min).** Three arms. Nonce per trial. Sink as the only evidence; why
   reply-text matching produces false positives (the refusal echo). Wilson intervals
   and the five verdicts. Delivery measured separately from compliance.
3. **Targets (4 min).** The four reference agents, their tools, and the three
   configurations. Why vulnerable and hardened differ in architecture, not in a
   filter.
4. **Results (10 min).** The 4 × 3 table. The guardrail delta. Voice variance across
   passes (5/10 vs 9/10) and what N it implies. The expense agent: delivered 9–10/10,
   complied 2–3/10, chat 10/10. The camera oracle the control arm caught.
5. **Negative results (4 min).** Morse over an LED: 23 pulses in 4.6 s, five frames at
   1 fps. DTMF: sixteen symbols. Both kept as adapters so they can be re-measured.
6. **Live demo (8 min).** Load a reference target, preview the artifact, send ten
   trials with the control, show the verdict, flip to hardened, re-run, download the
   bundle.
7. **Defenses (4 min).** Guardrail after perception; schema quarantine; confirmation
   on the user channel; interlocks in code. Measured together as the hardened policy.
8. **Limits and questions (4 min).** One model, one day, N = 10, HTTP-delivered
   artifacts, reference agents not products, modeled device-name channel.

## Why DEF CON

The audience builds and breaks these agents. The talk gives them a measurement they can
repeat on their own system in an afternoon, and a tool that refuses to say "confirmed"
without a control. It also corrects a trend: taxonomies of "channels" that are labels
on a JSON key. We killed most of ours and say so.

## Demo requirements

Laptop, HDMI, no network needed for the pre-recorded fallback; a live model API for the
live run. Optional: the Arduino LED rig and a webcam for the Morse negative result.

## Release

Crosswire source (Apache-2.0), knowledge-base entries AAT-0011 to AAT-0016 (CC BY 4.0),
run bundles and the driver script for every number in the talk.

## Speaker

[name] · [affiliation] · [bio ≤ 100 words] · [prior talks, if any]

## Has this been presented elsewhere

No. A Black Hat Arsenal/Briefings submission of the same work is planned; if both are
accepted the DEF CON version will carry the live rig demo and the negative results in
depth.
