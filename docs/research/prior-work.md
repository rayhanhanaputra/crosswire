# Crosswire prior-work survey

Researched 2026-11-01. Read-only against the repo. Nothing committed.

## How to read this file

Every entry has a status.

- **VERIFIED**: I fetched the URL and the page said what I cite.
- **PARTIAL**: I fetched it, but only the abstract, a docs page or source code. The detail beyond that is not confirmed.
- **UNVERIFIED**: I could not fetch the primary page. The claim comes from a search result or a secondary source.

"Relation" is one of covered, partly covered, or not covered, followed by the channel.

I checked the draft in `docs/deck/slides/coverage.html`, `docs/deck/slides/prior.html` and `docs/cfp/defcon-35.md`. The verdict on each claim is in the last two sections.

---

## 1. Calendar, email, document and agent-handoff attacks on assistants

### SafeBreach: "Invitation Is All You Need" (Gemini)

- **Authors:** Ben Nassi, Stav Cohen, Or Yair (Tel Aviv University, Technion, SafeBreach).
- **Venue:** arXiv 2508.12175, 16 Aug 2025. Talk at Black Hat USA, 6 Aug 2025, and DEF CON 33. (Talk dates are from press coverage, UNVERIFIED. The arXiv page is VERIFIED.)
- **URL:** <https://arxiv.org/abs/2508.12175> and <https://arxiv.org/pdf/2508.12175>
- **What it did:** Injected instructions through a calendar invite title into Gemini web, mobile and Google Assistant. It used the "when the user types thanks" delayed trigger, short-term context poisoning, tool misuse, long-term memory, and Google Home actions such as boiler on and windows open. I confirmed these in the PDF text (listings with "Do this when the user types thanks", "boiler", "windows").
- **Relation:** Covered for calendar, email, delayed trigger, and smart-home sink on Gemini. Partly covered for memory and agent-to-agent invocation. The paper also names voice (Google Assistant) as an entry surface. This is the closest prior art to the handoff and delayed-trigger channel.
- **Status:** VERIFIED (arXiv abstract plus PDF text).

### Google response to SafeBreach

- **Org:** Google. Blog post "Mitigating prompt injection attacks with a layered defense strategy", 13 Jun 2025.
- **URL:** <https://blog.google/security/mitigating-prompt-injection-attacks/> (redirected from security.googleblog.com/2025/06/...).
- **What it did:** Lists five layers: injection content classifiers, security thought reinforcement, markdown sanitization and URL redaction, a user confirmation framework, and end-user notifications. It names calendar event deletion and Gmail summaries as examples.
- **Relation:** A vendor defense for calendar and email. The SafeBreach paper says it disclosed on 22 Feb 2025 and that Google deployed mitigations (PDF text). Press says Google tested all scenarios internally. That press claim is UNVERIFIED.
- **Status:** VERIFIED.

### Rehberger: Gemini memory and delayed tool invocation

- **Author:** Johann Rehberger (wunderwuzzi), Embrace The Red.
- **Posts:**
  - 10 Feb 2025, "Hacking Gemini's Memory with Prompt Injection and Delayed Tool Invocation". <https://embracethered.com/blog/posts/2025/gemini-memory-persistence-prompt-injection/>
  - 22 Feb 2024, "Google Gemini: Planting Instructions For Delayed Automatic Tool Invocation". <https://embracethered.com/blog/posts/2024/llm-context-pollution-and-delayed-automated-tool-invocation/>
- **What it did:** The 2025 post hides instructions in a document the user asks Gemini to summarize. A later "yes" from the user triggers the memory tool, and false memories persist across sessions. The 2024 post uses an email to plant an instruction that fires on the next turn through the Workspace extension.
- **Relation:** Covered for memory poisoning, delayed triggers, and document and email delivery. These are two of the claims the draft marks "new".
- **Status:** VERIFIED.

### Rehberger: other relevant write-ups

- **Google Docs AI**, 12 Jul 2023. Hidden text in a Google Doc hijacked the AI summary into a fake "Malware detected" scam. Reported 22 Jun 2023, closed "Won't Fix". <https://embracethered.com/blog/posts/2023/google-docs-ai-scam/> . Covered: document. VERIFIED.
- **ZombAIs**, 24 Oct 2024. A web page made Claude Computer Use download and run a Sliver C2 binary. <https://embracethered.com/blog/posts/2024/claude-computer-use-c2-the-zombais-are-coming/> . Covered: computer-use screen. VERIFIED.
- **Blog index** lists more: ChatGPT memory attacks (May 2024, Sep 2024 SpAIware, Aug 2025), ChatGPT Operator (Feb 2025), AI ClickFix (May 2025), M365 Copilot ASCII smuggling (Aug 2024), "Computer-Use and TOCTOU" (Jun 2026). <https://embracethered.com/blog/> . I read the titles and dates from the index only. The posts were not opened. PARTIAL.
- The index shows no posts on voice or calendar.

### EchoLeak (Microsoft 365 Copilot, CVE-2025-32711)

- **Authors:** Pavan Reddy, Aditya Sanjay Gujral (case study). Found by Aim Security.
- **Venue:** arXiv 2509.10540, 6 Sep 2025. <https://arxiv.org/abs/2509.10540>
- **What it did:** One crafted email caused zero-click data exfiltration from Copilot. The chain bypassed Microsoft's XPIA classifier and link redaction.
- **Relation:** Covered: email, with a vendor product as the target and a vendor classifier defeated.
- **Status:** VERIFIED (arXiv abstract). The Aim Labs original write-up was not fetched.

### Morris II: "Here Comes The AI Worm"

- **Authors:** Stav Cohen, Ron Bitton, Ben Nassi. arXiv 2403.02817, Mar 2024. <https://arxiv.org/abs/2403.02817>
- **What it did:** Self-replicating adversarial prompts spread between GenAI email assistants through RAG.
- **Relation:** Covered: email, and agent-to-agent propagation. Image and audio variants are not confirmed from the abstract.
- **Status:** VERIFIED (abstract).

### Greshake et al.: "Not what you've signed up for"

- **Authors:** Kai Greshake, Sahar Abdelnabi, Shailesh Mishra, Christoph Endres, Thorsten Holz, Mario Fritz.
- **Venue:** arXiv 2302.12173, Feb 2023, revised May 2023. The arXiv page does not state a venue. <https://arxiv.org/abs/2302.12173>
- **What it did:** Named and taxonomised indirect prompt injection. Demonstrated on Bing Chat (GPT-4) and code-completion engines, including data theft and worming.
- **Relation:** Covered: the whole indirect class, for retrieved text and documents. Not covered: audio, phone, device fields. garak's resume and report probes cite Greshake's "inject-my-pdf" post.
- **Status:** VERIFIED.

### PDF peer-review injection (hidden white text)

- **Authors:** Janis Keuper, "Prompt Injection Attacks on LLM Generated Reviews of Scientific Publications". arXiv 2509.10248, Sep 2025. <https://arxiv.org/abs/2509.10248>
- **What it did:** Measured how simple hidden injections move LLM review scores, up to "up to 100% acceptance scores" on 1,000 ICLR reviews.
- **Relation:** Covered: PDF. Related papers (arXiv 2508.20863 "Publish to Perish", arXiv 2511.01287) were seen only in search results. UNVERIFIED for those two.
- **Status:** VERIFIED for 2509.10248.

---

## 2. Benchmarks for indirect injection

### AgentDojo

- **Authors:** Edoardo Debenedetti, Jie Zhang, Mislav Balunovic, Luca Beurer-Kellner, Marc Fischer, Florian Tramer (ETH Zurich and Invariant Labs).
- **Venue:** arXiv 2406.13352, Jun 2024. The arXiv page names no venue. The lead brief says NeurIPS 2024 (Datasets and Benchmarks). I could not confirm that from the page. <https://arxiv.org/abs/2406.13352>
- **What it did:** 97 tasks and 629 security cases. The PDF says: "We implement four environments (Workspace, Slack, Travel Agency and e-banking)". The workspace environment has an email inbox, a calendar and a cloud drive.
- **Relation:** Partly covered: email, calendar and drive content, as text planted in tool outputs inside a simulated environment. Not covered: real file formats, audio, phone, device fields.
- **Status:** VERIFIED (PDF text).

### InjecAgent

- **Authors:** Qiusi Zhan, Zhixiang Liang, Zifan Ying, Daniel Kang. ACL 2024 Findings. arXiv 2403.02691. <https://arxiv.org/abs/2403.02691>
- **What it did:** 1,054 cases, 17 user tools, 62 attacker tools, across "finance, smart home devices, email, and more". ReAct GPT-4 was hijacked 24% of the time.
- **Relation:** Partly covered: email and smart-home tool responses, as text. Not covered: device names as a field or any non-text channel.
- **Status:** VERIFIED.

### BIPIA

- **Authors:** Jingwei Yi, Yueqi Xie, Bin Zhu, Emre Kiciman, Guangzhong Sun, Xing Xie, Fangzhao Wu. KDD 2025. arXiv 2312.14197. <https://arxiv.org/abs/2312.14197>
- **What it did:** First indirect-injection benchmark. Tasks from the PDF's Table 1: email QA, web QA, table QA, summarization, code QA.
- **Relation:** Partly covered: email and document text. Text only.
- **Status:** VERIFIED (PDF text).

### WASP

- **Authors:** Ivan Evtimov, Arman Zharmagambetov, Aaron Grattafiori, Chuan Guo, Kamalika Chaudhuri (Meta). arXiv 2504.18575, Apr 2025. <https://arxiv.org/abs/2504.18575>
- **What it did:** Built on VisualWebArena (GitLab and Reddit clones). The attacker posts an issue or comment. Agents tested include Claude Computer Use and VisualWebArena agents with set-of-marks screenshots. Partial attack success up to 86%, but few full completions ("security by incompetence").
- **Relation:** Covered: computer-use screen, on web pages. Not covered: other channels. The PDF notes agents without a memory feature were out of scope.
- **Status:** VERIFIED (PDF text).

### VPI-Bench

- **Authors:** Tri Cao, Bennett Lim, Yue Liu, Yuan Sui, Yuexin Li, Shumin Deng, Lin Lu, Nay Oo, Shuicheng Yan, Bryan Hooi. arXiv 2506.02456, accepted at ICLR 2026. <https://arxiv.org/abs/2506.02456>
- **What it did:** 306 test cases on five platforms with visual injections for computer-use and browser-use agents. Up to 51% (computer-use) and 100% (browser-use) deception on some platforms. Per the search result, the platforms include a messenger and an email client.
- **Relation:** Covered: computer-use screen. This is the "one benchmark" on the draft's slide, and it is not the only one.
- **Status:** VERIFIED (abstract).

---

## 3. Open tooling

### garak (NVIDIA): latentinjection probes

- **URL:** <https://github.com/NVIDIA/garak/blob/main/garak/probes/latentinjection.py> (I read the source).
- **What it does:** Classes include LatentInjectionReport, LatentInjectionResume, LatentInjectionTranslationEnFr/EnZh, LatentInjectionFactSnippetEiffel/Legal, LatentJailbreak, LatentWhois. They embed an instruction inside text that looks like a report, resume, translation request or whois record. Two cite Greshake's "inject-my-pdf" post.
- **Relation:** Partly covered: document and email-like content as plain text. Not covered: real file formats, audio, phone, devices.
- **Status:** VERIFIED (source).

### PyRIT (Microsoft)

- **URL:** <https://github.com/microsoft/PyRIT> (the old Azure/PyRIT path now shows an almost empty tree).
- **What it does:** I read the source of these converters in `pyrit/converter/`:
  - `pdf_converter.py`: makes a PDF from text, or overlays text on an existing PDF. The source comment says injected text "defaults to white so it stays hidden on a typical page".
  - `word_doc_converter.py`: injects content into a Word document.
  - `qr_code_converter.py`: text to QR code image.
  - `add_image_text_converter.py`, `lsb_image_stego_converter.py`, `image_overlay_converter.py`: text on image, steganography.
  - `azure_speech_text_to_audio_converter.py`, `audio_white_noise_converter.py`, `audio_frequency_converter.py`: speech and audio perturbation.
  - Also `pyrit/executor/workflow/xpia.py`: a Cross-Domain Prompt Injection Attack workflow with a processing callback and a scorer.
- **Relation:** Covered as converters: PDF, Word, QR, image text, TTS. Partly covered as a harness: XPIA runs plant, then trigger, then score. Not covered: ICS and EML files, live SIP, device fields. I did not run any of it.
- **Status:** VERIFIED (source files). The converter docs page returned 404, so I did not read prose docs.

### promptfoo

- **URLs:** <https://www.promptfoo.dev/docs/red-team/plugins/indirect-prompt-injection/> and <https://www.promptfoo.dev/docs/red-team/strategies/>
- **What it does:** The indirect-prompt-injection plugin injects payloads into a template variable you name. Listed surfaces: RAG context, email body or subject, user profile or CRM notes, support tickets. Strategies page lists "Audio Encoding", "Image Encoding", "Video Encoding" (text rendered into media, then base64). No PDF or document strategy listed.
- **Relation:** Partly covered: email as a text variable. Covered: image and audio file to an API. Not covered: PDF, calendar, phone, devices.
- **Status:** VERIFIED (docs pages, through a summariser).

### Meta Llama Prompt Guard 2 and Llama Guard 3

- **URLs:** <https://huggingface.co/meta-llama/Llama-Prompt-Guard-2-86M> ; <https://github.com/meta-llama/PurpleLlama/blob/main/Llama-Guard3/README.md>
- **What it does:** Prompt Guard 2 is a text-only classifier ("benign" or "malicious"), 512-token window, for explicit override attempts. Llama Guard 3 has 8B and 1B text models and an 11B-vision model for text plus image prompts (a safety classifier, not an injection detector).
- **Relation:** Prompt Guard: chat text and extracted document text only. Llama Guard 3 Vision: images, for safety categories. Neither covers audio or protocol fields.
- **Status:** VERIFIED (model card and README).

### NVIDIA NeMo Guardrails

- **URL:** <https://docs.nvidia.com/nemo/guardrails/latest/index.html>
- **What it does:** Five rail types: input, retrieval, dialog, execution, output. Jailbreak detection by self-check, heuristics, and a NemoGuard NIM. A multimodal content-safety tutorial is linked. No audio or document-format support is described.
- **Relation:** Chat text and retrieved text. Partly covered for multimodal safety (not injection).
- **Status:** PARTIAL (overview page only).

---

## 4. Vendor products

### Microsoft Prompt Shields and Spotlighting

- **URL:** <https://learn.microsoft.com/en-us/azure/ai-services/content-safety/concepts/jailbreak-detection> (page dated 2026-10-14).
- **What it does:** Two shields. User prompt attacks, and document attacks for "documents, emails, web pages". The input is text in a `documents` array (up to five). For Foundry agents it can scan tool responses (Preview). Document attack detection is off by default. Models are "primarily trained and evaluated on English". Spotlighting base64-encodes document content to mark it untrusted.
- **Relation:** Covered as a vendor product: document, email and web content, after text extraction. Not covered: audio, phone, device fields, images.
- **Status:** VERIFIED.
- **Paper:** Hines et al., "Defending Against Indirect Prompt Injection Attacks With Spotlighting", arXiv 2403.14720, Mar 2024. <https://arxiv.org/abs/2403.14720> . ASR from over 50% to under 2% on GPT-family models. VERIFIED.

### Lakera (Guard / "AI Guardrails")

- **URL:** <https://docs.lakera.ai/guard>
- **What it does:** Screens text, including structured text. "Audio Defense screens audio input for prompt attacks and is available in Early Access." Image defense is "coming soon". Marketing pages claim PDFs and fetched content too; I did not open them.
- **Relation:** A vendor product with an audio screen in early access. This contradicts "vendor none" for audio input.
- **Status:** VERIFIED for the docs page. Marketing claims UNVERIFIED.

### Anthropic computer use

- **URL:** <https://platform.claude.com/docs/en/agents-and-tools/tool-use/computer-use-tool>
- **What it does:** States that instructions on webpages or in images can override the user's. Says classifiers scan tool results such as screenshots for injections and steer the model to confirm with the user.
- **Relation:** A vendor product with a screenshot injection classifier. The draft says vendor "none" for the computer-use screen. That is wrong.
- **Status:** VERIFIED.

### Hosted voice-agent security testing

- **Hamming:** guide "Voice Agent Prompt Injection Testing: A Security Runbook for Phone Calls", 18 Jun 2026. <https://hamming.ai/resources/voice-agent-prompt-injection-testing-guide> . A guide with six layers and pass thresholds. It does not say how calls are placed. Product page returned 404. PARTIAL.
- **Cekura:** <https://www.cekura.ai/discover/prompt-injection-testing-voice-agents> . Runs each attack "as a simulated call", real phone call in voice mode. Mock tools return planted payloads. Integrations listed: LiveKit, Pipecat, Vapi, Retell, ElevenLabs, Telnyx. Does not mention SIP. $0.25 per voice minute. VERIFIED (page via summariser).
- **RedCaller** and TestMuAI: named in a search result only. UNVERIFIED.
- **Relation:** Covered: live phone call, as closed hosted products.

---

## 5. Audio, speech and telephony

### AudioHijack

- **Authors:** Meng Chen, Kun Wang, Li Lu, Jiaheng Zhang, Tianwei Zhang. arXiv 2604.14604, 16 Apr 2026, accepted at IEEE S&P 2026. <https://arxiv.org/abs/2604.14604>
- **What it did:** Adversarial audio hijacks 13 large audio-language models with 79 to 96% success on unseen contexts. Also induced "commercial voice agents from Mistral AI and Microsoft Azure" to take unauthorized actions.
- **Relation:** Covered: audio-native injection against commercial voice agents, by optimised adversarial audio. Not covered: plain spoken instructions, SIP, an ASR text pipeline. The abstract does not discuss telephony.
- **Status:** VERIFIED (abstract).

### Piggybacking on Perception

- **Authors:** Mingxiao Liu et al. arXiv 2607.28165, 30 Jul 2026. <https://arxiv.org/abs/2607.28165>
- **What it did:** Mixes spoken malicious instructions into the user's own speech. Benchmark "AudioAgentSecurity" (8 scenarios, 10 attack patterns), 11 agents including Gemini 3 Pro and GPT-4o-audio. Average ASR 69.10% on Gemini 3 Pro. Defense called CADV. Real-world test on a Doubao phone.
- **Relation:** Covered: ambient and concurrent speech to audio agents, including a benchmark. This is close to the draft's "words a television says in the next room".
- **Status:** VERIFIED (abstract).

### Bagdasaryan et al.: "(Ab)using Images and Sounds"

- **Authors:** Eugene Bagdasaryan, Tsung-Yin Hsieh, Ben Nassi, Vitaly Shmatikov. arXiv 2307.10490, Jul 2023. <https://arxiv.org/abs/2307.10490>
- **What it did:** Adversarial perturbations in an image or audio clip steer LLaVA and PandaGPT. Search coverage says Black Hat Europe 2023 (UNVERIFIED).
- **Relation:** Covered: image and audio files, on open models.
- **Status:** VERIFIED (abstract).

### "For Prompt Injection, Press 1: Hacking AI Voice Agents"

- **Presenter:** Willie Zhang (ProCircular), DEF CON 34 AI Village poster, 8 Aug 2026.
- **URLs:** <https://aivillage.org/posters/for-prompt-injection-press-1-hacking-ai-voice-agents/> and <https://se.community/event/for-prompt-injection-press-1-hacking-ai-voice-agents/>
- **What it did:** An open-source tool that places real phone calls to voice agents, speaks 20 attack scenarios by text-to-speech, transcribes replies, and judges them by pattern match plus an LLM judge. The scenarios map to five OWASP LLM Top 10 categories.
- **Relation:** Covered: live phone call, open tooling, one year before the DEF CON 35 talk. The page names no repo link, no tool name and no platforms. I did not look at the tool. This directly contradicts "a live phone call: open tools none" and "SIP, new".
- **Status:** VERIFIED (both pages say the same). Repo not found.

### ScamAgents

- **Author:** Sanket Badhe. arXiv 2508.06457. <https://arxiv.org/abs/2508.06457>
- **What it did:** An LLM agent that writes scam-call scripts and speaks them by TTS.
- **Relation:** Not covered. It is about LLMs as callers, not injection of voice agents. Listed only so nobody cites it as prior art.
- **Status:** VERIFIED.

### Gap

I found no peer-reviewed paper on injection through an ASR front end that feeds a text-only agent, and no work on ChatGPT voice or Gemini Live injection. Two searches returned nothing. Absence in my searches is not proof of absence.

---

## 6. Vision and physical

### CHAI

- **Authors:** Luis Burbano, Diego Ortiz, Qi Sun, Siwei Yang, Haoqin Tu, Cihang Xie, Yinzhi Cao, Alvaro A. Cardenas. arXiv 2510.00181, accepted at IEEE SaTML. <https://arxiv.org/abs/2510.00181>
- **What it did:** Optimised misleading signs hijack vision-language agents on drones, vehicles and a robot car.
- **Relation:** Covered: camera and printed sign. Already credited in the draft.
- **Status:** VERIFIED.

### Physical Prompt Injection Attacks on LVLMs

- **Authors:** Chen Ling, Kai Hu, Hangcheng Liu, Xingshuo Han, Tianwei Zhang, Changhai Ou. arXiv 2601.17383, 24 Jan 2026. <https://arxiv.org/abs/2601.17383>
- **What it did:** Black-box typographic instructions on physical objects. 10 LVLMs, up to 98% success, robust to distance, angle and light.
- **Relation:** Covered: camera and physical text. The draft cites only CHAI. A reviewer will find this one.
- **Status:** VERIFIED (abstract).

### AgentTypo

- **Authors:** Yanjie Li, Yiming Cao, Dong Wang, Bin Xiao. arXiv 2510.04257, Oct 2025. <https://arxiv.org/abs/2510.04257>
- **What it did:** Black-box typographic injection in web images, tested on the VWA-Adv benchmark. GPT-4o ASR 0.23 to 0.45, up to 0.68 combined.
- **Relation:** Covered: computer-use screen and images.
- **Status:** VERIFIED.

### Cisco: "Reading between the pixels"

- **Authors:** Ravikumar Balakrishnan, Amy Chang, 15 Apr 2026. <https://blogs.cisco.com/ai/reading-between-the-pixels-assessing-prompt-injection-attack-success-in-images>
- **What it did:** Rendered 1,000 SALAD-Bench prompts as images and tested four VLMs. QR codes appear only as a "possible delivery route". They were not tested.
- **Relation:** Typographic images covered. QR code: mentioned, not measured. I found no paper on QR-code injection.
- **Status:** VERIFIED.

### Not an A11y

- **Authors:** Rahul Deivasigamani, Sayeda Faatin Alvi, Derqui Andrea, Kaushal Punjabi, Stjepan Picek. arXiv 2608.08939, 9 Aug 2026. <https://arxiv.org/abs/2608.08939>
- **What it did:** Injection through the Android accessibility tree and screenshots into mobile agents. ASR 0.822 for MobileRun with Gemma4:31B.
- **Relation:** Covered: computer-use screen. It is the closest thing to "protocol field" injection I found: unsanitised accessibility metadata. It is not device names.
- **Status:** VERIFIED (abstract).

---

## 7. Defenses and frameworks

- **CaMeL.** Debenedetti, Shumailov, Fan, Hayes, Carlini, Fabian, Kern, Shi, Terzis, Tramer. "Defeating Prompt Injections by Design". arXiv 2503.18813, Mar 2025. <https://arxiv.org/abs/2503.18813> . Separates control flow from data flow and uses capabilities. 77% of AgentDojo tasks with provable security, against 84% undefended. Relation: the hardened policy in the draft (quarantine, withheld tools, interlocks) is the same family of ideas. VERIFIED.
- **Dual LLM pattern.** Simon Willison, 25 Apr 2023. <https://simonwillison.net/2023/Apr/25/dual-llm-pattern/> . Privileged LLM, quarantined LLM, and a controller. VERIFIED.
- **Lethal trifecta.** Simon Willison, 16 Jun 2025. <https://simonwillison.net/2025/Jun/16/the-lethal-trifecta/> . Private data, untrusted content, and external communication. The post does not mention the dual LLM pattern. VERIFIED.
- **OWASP LLM01:2025.** <https://genai.owasp.org/llmrisk/llm01-prompt-injection/> . Covers indirect injection, hidden instructions in images, and a resume scenario. Says injection "do[es] not need to be human-visible". Audio is not named. VERIFIED.
- **Google layered defense.** See section 1.

---

## 8. Protocol and device-name injection

I searched for prior reports on Home Assistant entity names, Wi-Fi SSIDs, Bluetooth names, MQTT topics and SIP headers aimed at an LLM agent. I found none.

What I did find:

- Home Assistant's own docs and community templates place entity names and aliases in the LLM prompt, and HA's blog calls prompt injection "a real concern" without describing a name-based attack. The pages are real but I only read search summaries. PARTIAL. <https://developers.home-assistant.io/docs/core/llm/> ; <https://www.home-assistant.io/blog/2024/06/07/ai-agents-for-the-smart-home/>
- SSID and Bluetooth name injection exists as classic code injection into IoT software (MDPI review, <https://www.mdpi.com/1424-8220/23/13/6067> ). No LLM. UNVERIFIED beyond the search summary.
- Not an A11y (section 6) is the nearest LLM-agent case.
- InjecAgent has smart-home tools, but the payload is in a tool response.

Conclusion: the device-name and protocol-field channel is the one row where "no prior work found" stands. Say "I found none" and not "there is none".

---

## 9. Corrected coverage table

| What the agent reads | Prior research | Open tooling | Vendor product | What Crosswire adds |
|---|---|---|---|---|
| Chat text | Greshake 2023 and many others | garak, PyRIT, promptfoo | Prompt Shields, Prompt Guard 2, Lakera, NeMo Guardrails | Direct arm used only as the control and the guardrail comparison |
| Image or audio file sent to an API | Bagdasaryan 2023, AudioHijack 2026, Piggybacking 2026, AgentTypo, Cisco 2026 | PyRIT (image text, QR, TTS, audio converters), promptfoo (image, audio, video encoding) | Lakera Audio Defense (Early Access), Llama Guard 3 Vision (safety, not injection) | Nothing new in the channel. Sink-and-nonce scoring with a benign control is the addition |
| A live phone call | Voice-agent attacks: AudioHijack tests commercial voice agents. No telephony paper found | DEF CON 34 AI Village tool (calls, TTS, ASR, judge). Repo not found | Hamming and Cekura (real phone calls, hosted, closed) | An open SIP connector with a control arm and a tool-sink verdict. Not the first open phone-call tester |
| PDF, calendar invite, email | Greshake 2023, Rehberger Docs 2023 and Gemini 2024, SafeBreach 2025, EchoLeak 2025, Morris II 2024, Keuper 2025, AgentDojo, InjecAgent, BIPIA | PyRIT (PDF with hidden text, Word, XPIA workflow), garak latentinjection (text only), promptfoo (email as a variable) | Prompt Shields document attacks, Google classifiers, Lakera | Real attachments (ICS, EML, PDF) delivered to a running agent with a sink and a control. Not "none" in any column |
| Device names and protocol fields | None found. Nearest: Not an A11y (accessibility tree), InjecAgent (tool responses) | None found | None found | The channel itself, as modeled text. Stands as "no prior report found" |
| A computer-use agent's screen | VPI-Bench, WASP, AgentTypo, Not an A11y, Rehberger ZombAIs and TOCTOU | VPI-Bench and WASP code | Anthropic screenshot injection classifier | Nothing. The draft already says "no" |
| Camera, radio or sensor in the room | CHAI, Physical Prompt Injection (2601.17383), Piggybacking (ambient speech). No radio or sensor LLM work found | None found | None found | Camera as a tested channel with a control. LED rig is a negative result only |
| Memory and other agents | Rehberger (ChatGPT memory 2024, SpAIware, Gemini memory 2025), SafeBreach 2025, Morris II 2024, AgentDojo (tool outputs) | None found for delayed or memory triggers | User-confirmation layers (Google). No memory-specific test product found | A two-phase trial and a handoff channel inside a controlled harness. The attack class is not new |

---

## 10. Draft claims, checked

### Wrong or overstated

1. "No tool or vendor covers PDF, calendar or email injection." Wrong. PyRIT has PDF and Word converters and an XPIA workflow. Prompt Shields scans documents and emails. SafeBreach and EchoLeak attacked calendar and email in vendor products.
2. "A live phone call: open tools none; SIP, new." Wrong. The DEF CON 34 AI Village tool places real calls with TTS and ASR. Hamming and Cekura sell the same.
3. "Memory and other agents: yes, new." Wrong as a claim of novelty. Rehberger and SafeBreach did memory poisoning and delayed triggers in 2024 and 2025.
4. "Image or audio: vendor some" is understated. Lakera has an audio screen in Early Access. Prior research is thin on the slide, with no mention of AudioHijack.
5. "A computer-use agent's screen: one benchmark, vendor none." Wrong. VPI-Bench, WASP and AgentTypo exist, and Anthropic documents a screenshot classifier.
6. prior.html cites only CHAI for physical injection. Physical Prompt Injection (2601.17383) is also prior art.
7. The CFP says "a Burp-style Repeater and Intruder for perception paths" and the deck says "One tool". PyRIT already has converters and a workflow. The difference must be stated as controls and sinks, not as a missing tool.
8. The slide footer says "Vendor claims not verified." That is honest, but the table still states "none" as fact.

### Stands

1. Device names and protocol fields: I found no prior report. Say "none found".
2. Chat-text coverage by garak, PyRIT and promptfoo.
3. CHAI is credited correctly, and its result is described correctly.
4. The measurement contribution (nonce per trial, sink as evidence, control arm, Wilson intervals, guardrail placement) is not something I found elsewhere. I did not search the methods literature for it.
5. Negative results (Morse on an LED, DTMF) and the hardened-policy comparison. I did not search for them. CaMeL and the dual LLM pattern are related defenses and should be cited.
