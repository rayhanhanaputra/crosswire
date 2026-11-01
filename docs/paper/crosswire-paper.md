# Where the Guardrail Sits: Measuring Prompt Injection Through What Agents Perceive

**[Author], [affiliation], [email]** · Draft of 1 November 2026 · Target: a security workshop (AISec, SaTML or an S&P/USENIX workshop). Bracketed fields are for the author.

## Abstract

Prompt-injection classifiers are deployed on the chat path of tool-using language-model agents. The agents also act on inputs that never cross that path: the names of devices on a network, the audio of a phone call, the frame a camera describes, the text extracted from an invoice or a PDF, and the notes another agent left behind. We measure how much of the attack surface a chat-path classifier covers, and what moving it changes, with a procedure that counts a hit only when a forbidden tool call reaches an out-of-band sink carrying a per-trial nonce. Against six reference agents on one production model (`gpt-4o-mini`), 30 trials per arm with five paraphrases, a classifier on the chat path removed typed injections (29–30/30 to 0–16/30) and left the perception-path rate unchanged (21–30/30 on six of seven channels); the same classifier on the perception path reversed that. A hardened design with four switchable layers executed no privileged call in 210 trials. Removing the layers one at a time shows which one earned that: on the handoff-note channel the model attempted the privileged call in 13–30 of 30 trials under every model-side layer, and a four-line check in the tool handler was the only thing that stopped it (0/30 with it, 20/30 without). A benign control arm run beside every injected arm voided three of our own runs, including one where the agent unlocked a door on a quiet device name. We release the workbench, the reference agents, the driver script and every trial.

## 1. Introduction

A language-model agent takes a user's request, reads some state of the world, and calls tools. Prompt-injection defenses are placed where the request arrives. The state of the world arrives elsewhere: a home platform assembles an entity list from device names set by whoever owns the device; a voice assistant transcribes whatever is in the room; a camera agent describes a frame; an accounts-payable agent extracts fields from a rendered invoice; an inbox assistant reads a PDF, including the paragraph printed in white ink; a support-desk agent reads the note the previous shift's agent left on the ticket.

That a classifier cannot act on what it does not read is not in dispute. What is missing is a measurement: how large the gap is on a working agent, what happens to the same classifier when it is moved to the other path, and which of the layers in a "hardened" design actually produce the zero that such designs report. This paper supplies those three measurements, and a procedure for making them that does not depend on reading the model's reply.

Contributions:

1. **A verdict procedure** in which a hit is a forbidden tool call reaching a sink the tester controls, carrying the trial's nonce in its arguments. Reply text is never evidence. A benign control of the same shape runs beside every injected arm, with its own nonce; a hit in the control arm voids the run. Calls the target refuses in code are recorded with the refusing layer and reported as *attempted*, apart from *executed*.
2. **A placement comparison**: one prompt-injection classifier placed on the chat path, on the perception path, or on both, against the same agent, so the measured quantity is where the classifier sits and not whether one exists.
3. **A per-layer ablation** of a four-layer hardened design (schema quarantine, tool withholding, user-channel confirmation, code interlock), each layer switchable on its own.
4. **Negative and voided results**, kept in the record: a channel that cannot carry an instruction at camera frame rates, and three runs thrown out by their own control arm.

We claim no new attack class. Indirect prompt injection is Greshake et al.'s [1]; injection through calendar invites, email and memory against shipped products is Nassi et al. [2], EchoLeak [3] and Rehberger [4]; audio-native injection is Chen et al. [5]; physical signs against vision agents are Burbano et al. [6]. Our device-name channel has no prior report we could find, and we say "none found", not "first".

## 2. Related work

**Attacks.** Greshake et al. [1] introduced indirect prompt injection: instructions placed in content an application retrieves. Nassi, Cohen and Yair [2] demonstrated it through calendar invites and email against a shipped assistant; EchoLeak [3] showed a zero-click variant against a productivity assistant; Rehberger [4] showed persistence through agent memory and delayed tool invocation. Cohen, Bitton and Nassi [7] showed self-replicating injection across agents. Chen et al. [5] injected through audio against speech-native models; Burbano et al. [6] through printed signs against vision-language agents.

**Benchmarks and tools.** AgentDojo [8] and InjecAgent [9] score agents against injected tool outputs with utility and attack-success metrics; they test the text the agent reads back from tools rather than a sensor channel, and report success from the trajectory. PyRIT [10] converts text into PDFs, images and speech and drives multi-turn attacks; garak [11] and promptfoo [12] probe the chat path. A DEF CON 34 AI Village tool placed calls to voice agents [13]. None of these runs a benign control arm or separates attempted from executed calls.

**Defenses.** Spotlighting [14] marks untrusted text; Prompt Shields [15] is a deployed classifier; the dual-LLM pattern [16] and CaMeL [17] separate a privileged planner from a quarantined reader; tool-handler checks and user confirmation are standard practice. We implement a reference version of each as a switchable layer and measure them one at a time rather than together.

## 3. Method

### 3.1 Trial

A trial is one instruction, one nonce, one send, one observation. The instruction asks the agent to do something privileged with a value the attacker controls (an email address, an account, a door). The nonce `cw-xxxxxxxx` is placed in that value, so the nonce can reach a tool argument only if the model acted on the instruction. The instruction is encoded for the channel: a device name string, a synthetic-speech WAV, a rendered PNG sign, a rendered invoice PNG, a PDF with the instruction in white ink, or a handoff note posted to the agent's memory. It is sent to the agent's perception endpoint over HTTP or, for the phone channel, played as G.711 audio over a SIP call that the agent answers.

Every tool call the agent makes is POSTed to a sink the tester owns: name, arguments, whether the handler executed or refused it, the refusing layer's reason, the active layers and the request's session. A trial's evidence is the sink events of its own session. A **hit** is an executed forbidden tool whose arguments carry the nonce. An **attempt** is the same call refused in code. Response text and the actions the agent *reports* are recorded as hints and never count.

### 3.2 Arms and verdicts

Each run has three arms of N trials: **injected** (the instruction through the channel), **control** (a benign text of the same shape and length through the same channel, with its own nonce) and **direct** (the instruction typed into chat). Five paraphrases of the injected instruction and three of the control rotate by trial index. Rates are reported as k/N with 95 % Wilson intervals [18]. The verdict is `confirmed` (≥2 hits, clean control, injected and control intervals disjoint), `observed` (hits but not confirmed), `not_observed` (zero hits; a bound, never "safe"), `invalid` (a control-arm hit) or `error` (the target answered with an error, so nothing was measured).

### 3.3 Reference agents and builds

Six agents, each a real tool-calling model behind an HTTP API, written for this work: **home** (reads a device name in its entity list; forbidden tools `unlock_door`, `disarm_alarm`), **voice** (transcribes audio with a speech model; `send_email`), **camera** (describes a frame with a vision model; `disarm_alarm`), **expense** (extracts an invoice with a vision model; `pay_invoice`), **inbox** (reads mail and PDF attachments as text; `share_file`, `forward_email`) and **desk** (stores handoff notes per session and reads them on the next routine request; `issue_refund`). The voice agent also answers SIP calls.

Each agent runs in eight builds. `vulnerable`: one loop, every tool offered. `guardrail=chat`, `guardrail=perception`, `guardrail=both`: the same reference prompt-injection classifier (a tool-less model prompted to flag instructions addressed to the assistant) reading the user turn, the perceived text, or both; a flagged input is dropped. `hardened`: four layers. *Quarantine*: perception goes through a tool-less model into a fixed schema before the agent sees it. *Withhold*: privileged tools are not offered on the perception path; a call to one is refused and recorded. *Confirm*: a privileged tool needs a flag only the user channel sets. *Interlock*: a code check in the handler (the refund account must match the customer on the ticket; the payee must match the vendor record). Four further builds switch one layer off.

### 3.4 Procedure

The driver starts each build from one binary, checks its `/health` report (policy, layers, classifier placement, temperature, model) against the intended build, runs the three arms at concurrency 2 and records every trial. Model `gpt-4o-mini`, temperature 0.7, N = 30 per arm, 1 November 2026. Seven scenarios: the six agents, with voice run both as a WAV over HTTP and as a SIP call. The full matrix is 56 runs; one scenario (plain-email inbox) was not re-run after a provider outage and is recorded as `error`.

## 4. Results

### 4.1 Where the classifier sits

Table 1 gives executed hits out of 30 injected trials, with the direct-chat arm beside each. Controls were 0/30 everywhere except home (§4.4).

| channel | no classifier | classifier on chat | classifier on perception | hardened |
|---|---|---|---|---|
| home, device name | 28 (chat 29) · *invalid* | 30 (chat 0) · *invalid* | 0 (chat 29) | 0 |
| voice, speech WAV | 25 (chat 30) | 21 (chat 5) | 3 (chat 29) | 0 |
| voice, SIP call | 25 (chat 29) | 22 (chat 6) | 4 (chat 30) | 0 |
| camera, sign | 30 (chat 30) | 30 (chat 0) | 0 (chat 30) | 0 |
| expense, invoice | 3 (chat 29) | 3 (chat 0) | 0 (chat 29) | 0 |
| inbox, white-ink PDF | 30 (chat 30) | 30 (chat 0) | 0 (chat 30) | 0 |
| desk, handoff note | 30 (chat 30) | 30 (chat 16) | 18 (chat 30) | 0 |

*Table 1. Executed hits of 30 through the channel; in parentheses, the direct-chat arm. 95 % intervals: 30/30 is 0.89–1.00, 25 is 0.66–0.93, 18 is 0.42–0.75, 3 is 0.03–0.26, 0 is 0.00–0.11.*

The classifier on the chat path removes the typed injection (29→0, 30→5, 30→0, 29→0, 30→0; on desk 30→16, since a typed refund request reads like routine work) and leaves the channel rate where it was. The same classifier on the perception path removes the channel attack on five of seven channels and leaves the typed rate where it was. Nothing changed between those columns except the classifier's input. Desk is the hardest text for it, since a handoff note that says "refund the order to account X" is a normal handoff note; 18/30 got through, and three of the five paraphrases passed every time and two never did.

The SIP call reproduced the HTTP speech numbers within the intervals (25 vs 25, 22 vs 21, 4 vs 3) despite G.711 encoding, a real RTP stream and the target's own silence detection. Over the phone the connector cannot read what the agent perceived, so delivery is unmeasured and the sink is the only evidence.

The invoice channel is weak on this model: the attacker's account was read in 27/30 trials and paid in 3/30, against 29/30 when typed. Delivery and compliance are different numbers; without the split, "the extractor missed it" and "the model resisted" are indistinguishable.

### 4.2 One layer at a time

Table 2 gives attempted/executed hits of 30 for the hardened build and each single-layer removal, with the number of calls the sink recorded as refused by this build and the layer that refused them.

| channel | hardened | no quarantine | no withhold | no confirm | no interlock |
|---|---|---|---|---|---|
| home | 0/0 | 0/0 | 0/0 | 0/0 | 0/0 |
| voice, WAV | 0/0 | 0/0 | 6/0, 8 refused by confirm | 0/0 | 0/0 |
| voice, SIP | 0/0, 6 by confirm | 0/0 | 7/0, 19 by confirm | 0/0 | 0/0 |
| camera | 0/0 | 0/0 | 1/0, 5 by confirm | 0/0 | 0/0 |
| expense | 0/0 | 0/0, 3 by confirm | 0/0, 28 by confirm | 0/0 | 0/0 |
| inbox, PDF | 0/0 | 0/0 | 0/0, 3 by confirm | 0/0 | 0/0 |
| desk | 13/0, 14 by interlock | 30/0, 34 by interlock | 20/0, 20 by interlock | 18/0, 20 by interlock | **20/20** (0.49–0.81) |

*Table 2. Attempted/executed of 30 injected trials. Refused counts include calls without the nonce.*

With all four layers on, no privileged call executed in 210 injected trials across seven scenarios. Two different mechanisms produced that zero. On six channels the model rarely reached for a privileged tool at all; when it did, it was in a build with the withhold layer off, and the confirmation gate refused every call. No single removal produced an executed hit on those six, so those rows cannot rank the layers, and we do not.

Desk is different. The handoff note passes quarantine (the schema keeps "refund 240 to account X" as a pending item, because that is the content of a handoff note), passes withhold (the refund is issued on the next shift, triggered by a routine user request, on which path the refund tool is offered) and passes confirmation (that routine request is a user turn). The model attempted the refund in 13 to 30 of 30 trials under every hardened variant. The only layer that stopped it was the interlock, four lines in the handler that compare the refund account with the customer on the ticket: 0/30 with it, 20/30 without it. A memory channel bypasses perception-time defenses because the instruction is read in one request and acted on in another, on a path the designer trusted.

### 4.3 Paraphrase variance

Hits per paraphrase (six trials each) on the voice channel without a classifier were 6, 5, 5, 6, 3; with the classifier on chat, 6, 5, 5, 1, 4. On desk with the classifier on the perception path, 0, 6, 0, 6, 6; with the interlock off, 6, 2, 1, 6, 5. A result built from one prompt would have reported anywhere from 0/6 to 6/6 depending on which sentence the author wrote.

### 4.4 What the control arm caught

Three runs were voided by their own control arm and are kept in the record.

*Camera, 31 October.* The first oracle counted `mark_clear`; 10/10 benign signs produced it, because clearing a quiet scene is what the agent does on any frame. The rule was narrowed to `disarm_alarm` and the run repeated clean.

*Home.* One of thirty benign device names ("Hallway Thermo 3 - signal strong, battery 64%") made the agent turn on a light and unlock the front door, with no instruction in the input. The attack rates in the same runs were 28/30 and 30/30; the rows still read `invalid`; the rule has no exception for a large difference. The agent's prompt invites it to act on a new device, and at temperature 0.7 it sometimes does.

*Desk, first pass.* On "customer is happy with the replacement, please close the ticket", the agent issued a refund to the real customer in four builds, using the note's call reference as the ticket id. The prompt listed "issue a refund" among routine actions. The same pass exposed an engine defect: trial evidence was selected by time window, so with two trials in flight each trial's sink list carried the other's calls (260 refund events named another trial's nonce). Hit counts were unaffected, since a hit requires the trial's own nonce, but evidence must be a trial's own. Sink events now carry the request session and the engine keeps only that session's events. The prompt was tightened and the scenario re-run: controls 0/30, no cross-session events.

### 4.5 A channel that does not work

Morse on an LED at 12 words per minute is 37 on/off steps in 6.7 s, the shortest 100 ms. A 1 fps camera takes seven frames of it. The adapter emits the pulse timeline and drives a serial LED rig so the negative result can be re-measured with a fast sensor; the earlier version of this project sent decoded Morse text to the target and skipped the physical step.

## 5. Discussion

**Where to put the classifier.** The classifier is the same code in both columns of Table 1. Its coverage is its input. A deployment that routes only the user turn through it has measured the wrong thing; routing the perceived text through it is cheap and, on five of seven channels here, decisive. The desk row shows the limit: text that *is* an instruction by design cannot be classified away.

**Which layer earned the zero.** A hardened zero can be true while three of four layers contribute nothing on the channel that matters. On desk the layer that held is the one with no model in the loop. The other layers may matter against other attacks; a zero measured with every layer on cannot show that either way.

**Why count at the sink.** A refusal that quotes the attack back matches any text marker. Reported actions are claims. Counting at the tool with a nonce removes both false positives, and recording refusals separately removes a false negative: a defense that works in code is not mistaken for a model that declined.

**Why run a control.** Three of our own runs would have been reported as 10/10, 28/30 and 30/30 successes by a single-send analyzer. The control arm is the cheapest part of the procedure, and most tooling omits it.

## 6. Limitations

One model at one temperature on one day; the rates are `gpt-4o-mini`'s and the method is not. Two further models were started: `deepseek-flash` completed three home builds (0/30 through the channel, 0–2/30 typed; it declines however asked) before the run was stopped for cost, and Gemini could not be run. N = 30 gives intervals about ±0.11 wide at the extremes. The agents are reference implementations written for this work, not products; the voice-platform terms we surveyed do not permit testing products without consent, and none was tested. The SIP connector is UDP with digest authentication, no TLS or SRTP, run against the reference target's own listener. The sink needs the target's cooperation; a black-box product can be tested only where its tools can be pointed at a URL the tester controls, which is how most voice-agent platforms define custom tools. Evidence lists for scenarios run before the session fix may contain a concurrent trial's calls; counts are unaffected. The device-name channel is modeled as text; its 28/30 backs no claim about a radio path.

## 7. Ethics

All targets are our own. No product, vendor or third party was tested. The reference agents are released so that others can measure their own agents with the same procedure; the tool refuses to report `confirmed` without a control arm.

## 8. Conclusion

A prompt-injection classifier stops what it reads. Moved from the chat path to the perception path on the same agent, it stopped a different attack and let the other one through, with nothing else changed. A hardened design's zero was earned, on the channel that mattered, by a code check and not by the three model-side layers in front of it. Both findings depended on counting at the tool rather than in the reply, and on a control arm that can void a run. We release the workbench, the agents, the driver and every trial.

## Availability

Crosswire (Apache-2.0): `crosswire/` in [repository]. Results: `crosswire/docs/results/2026-11-01.md` and the JSON beside it. Driver: `crosswire/docs/results/experiment.py`.

## References

[1] K. Greshake, S. Abdelnabi, S. Mishra, C. Endres, T. Holz, M. Fritz. Not what you've signed up for: Compromising real-world LLM-integrated applications with indirect prompt injection. arXiv:2302.12173, 2023.
[2] B. Nassi, S. Cohen, O. Yair. Invitation is all you need: Promptware attacks against LLM-powered assistants in production. arXiv:2508.12175, 2025.
[3] Aim Security. EchoLeak: zero-click prompt injection in Microsoft 365 Copilot. 2025.
[4] J. Rehberger. Embrace The Red (blog): memory persistence and delayed tool invocation in agents, 2024–2025.
[5] Chen et al. AudioHijack / Piggybacking on perception: audio-native prompt injection. arXiv:2604.14604, 2026.
[6] Burbano et al. CHAI: physical prompt injection against vision-language agents. arXiv:2510.00181, 2025.
[7] S. Cohen, R. Bitton, B. Nassi. Here comes the AI worm (Morris II). arXiv:2403.02817, 2024.
[8] E. Debenedetti et al. AgentDojo: a dynamic environment to evaluate prompt injection attacks and defenses for LLM agents. arXiv:2406.13352, 2024.
[9] Q. Zhan et al. InjecAgent: benchmarking indirect prompt injections in tool-integrated LLM agents. arXiv:2403.02691, 2024.
[10] Microsoft. PyRIT: Python Risk Identification Toolkit. github.com/Azure/PyRIT.
[11] NVIDIA. garak: LLM vulnerability scanner. github.com/NVIDIA/garak.
[12] promptfoo. github.com/promptfoo/promptfoo.
[13] DEF CON 34 AI Village, voice-agent calling tool, 2026. [confirm title and authors]
[14] K. Hines et al. Defending against indirect prompt injection attacks with spotlighting. arXiv:2403.14720, 2024.
[15] Microsoft. Azure AI Content Safety: Prompt Shields. Product documentation.
[16] S. Willison. The dual LLM pattern for building AI assistants that can resist prompt injection. 2023.
[17] E. Debenedetti et al. Defeating prompt injections by design (CaMeL). arXiv:2503.18813, 2025.
[18] E. B. Wilson. Probable inference, the law of succession, and statistical inference. JASA 22(158), 1927.

*References 5, 6 and 13 carry the identifiers from `docs/research/prior-work.md`; confirm titles and author lists against the arXiv pages before submission.*
