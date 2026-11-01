# Voice platforms: can Crosswire's SIP connector point at them?

Researched 2026-11-01. Not legal advice. I read the terms as plain text.

Status tags: **VERIFIED** means I fetched the page and it said this. **UNVERIFIED** means I could not fetch the primary page.

## Short answer

Five of six platforms take inbound SIP. All six let the tester run the tool code. No platform has a clause that allows security testing, and two terms ban "testing vulnerabilities of the Services". Get written permission first.

One technical blocker. The connector in `crosswire/internal/connector/sip.go` speaks plain SIP over UDP with digest auth and RTP. It has no TLS and no SRTP. Bland, ElevenLabs and OpenAI document TLS transports. So the connector cannot reach them directly. A local SIP proxy or an SBC that bridges UDP to TLS and SRTP is needed.

## Table

| Platform | Inbound SIP to an agent | Tools as webhooks the tester controls | Terms on security testing |
|---|---|---|---|
| Vapi | Yes | Yes | Clause bans testing for vulnerabilities of the Services |
| Retell AI | Yes | Yes | Not found |
| ElevenLabs Agents | Yes (trunk) | Yes | Not found |
| OpenAI Realtime over SIP | Yes (docs page not fetched) | Yes, in your own code | Not retrieved |
| Twilio ConversationRelay | Yes for Twilio SIP. Combination not stated | Yes, your WebSocket runs the LLM and tools | AUP bans exploiting vulnerabilities and bypassing security |
| Bland AI | Yes | Yes | Bans interference with security features. No pentest clause |

## Vapi

- **Inbound SIP: yes. VERIFIED.** https://docs.vapi.ai/advanced/sip says you attach a `sipUri` of the form `sip:username@<sip-host>` to an assistant, then dial it from a softphone. Quote: "The assistant will answer your call. No authentication or SIP registration is required." A Vapi SIP number is "inbound calls only". The trunk guide, https://docs.vapi.ai/advanced/sip/sip-trunk , routes provider calls to a regional URI such as `{phoneNumber}@<credential_id>.sip.vapi.ai`. It says to allowlist the region's signaling IPs. The pages did not state the transport. UNVERIFIED for UDP.
- **Tools as webhooks: yes. VERIFIED.** https://docs.vapi.ai/tools/custom-tools : the tool has a `server.url`, "The endpoint where your function is hosted". Vapi POSTs `message.type = tool-calls` with `toolCallList`. The server must answer HTTP 200 with a `results` array of flat strings.
- **Terms. VERIFIED.** https://vapi.ai/terms-of-service , "Last Updated" 14 Sep 2026.
  - Section 2(d)(11): "try to detect, scan, or test any vulnerabilities of the Services".
  - Also "breach or override any security or authentication protections for the Services" and "circumvent the security of the Services" (2(d)(9)).
  - Section 2(d)(7) bans reverse engineering of the Vapi Software.
  - No penetration testing clause and no research safe harbour.
  - My reading: prompt-injecting your own assistant is testing your own application. But the 2(d)(11) wording is not limited to the platform's own code. A reviewer could argue either way. Ask Vapi in writing.

## Retell AI

- **Inbound SIP: yes. VERIFIED.** https://docs.retellai.com/deploy/custom-telephony offers two routes.
  - Elastic SIP trunk: "set the origination SIP URI to Retell's SIP server, `sip:sip.retellai.com`", then import the number.
  - Dial to a SIP URI: call the Register Phone Call API for a `call_id`, then dial `sip:{call_id}@sip.retellai.com` within five minutes.
  - Transport is not stated on the page I read.
- **Tools as webhooks: yes. VERIFIED.** https://docs.retellai.com/build/single-multi-prompt/custom-function : the URL "must be a valid, publicly reachable URL". Localhost and private IPs are blocked, so use a tunnel. Retell sends JSON with `name`, `call` (including the transcript) and `args`, and an `X-Retell-Signature` HMAC header. Results are capped at 15,000 characters.
- **Terms. VERIFIED.** https://www.retellai.com/terms-of-service , "Last Updated: October 25, 2026".
  - Security testing clause: not found.
  - Section 10.1: bans "reverse engineering, decompiling, or otherwise attempting to derive source code or underlying models".
  - Section 4.1 bans any use that "disrupts the integrity of any Retell AI system or service".
  - Section 10.1(b) bans scraping or extracting data, voice samples or model outputs by automated means. Read this before running large automated trials.

## ElevenLabs Conversational AI (Agents)

- **Inbound SIP: yes. VERIFIED.** https://elevenlabs.io/docs/agents-platform/phone-numbers/sip-trunking : calls from your SIP trunk go to a configured INVITE address.
  - `sip:sip.rtc.elevenlabs.io:5060;transport=tcp`
  - `sip:sip.rtc.elevenlabs.io:5061;transport=tls`
  - `sip:sip.rtc.elevenlabs.io:5060;transport=udp`, "experimental and for testing only"
  - The URI needs an identifier before the `@`, such as an E.164 number, for example `sip:+19991234567@sip.rtc.elevenlabs.io:5060`.
- **Tools as webhooks: yes. VERIFIED, thinly.** https://elevenlabs.io/docs/agents-platform/customization/tools/server-tools : you set a URL, method and headers, and the assistant fills the parameters. The example is a GET to a weather API. The page I got used older "assistant" wording, so check the current page for the request body format.
- **Terms. VERIFIED.** https://elevenlabs.io/terms-of-use , "Last Updated: 09 October 2026". No clause on security testing, probing, reverse engineering or circumventing security. Only Section 5(b) (limited licence, anything else needs written permission). I did not read ElevenLabs' separate prohibited-use policy.

## OpenAI Realtime API over SIP

- **Inbound SIP: yes. Primary page not fetched.** The official guide, https://developers.openai.com/api/docs/guides/realtime-sip , appeared in a search result but returned 404 to my fetch, both with the fetch tool and with curl. What I saw comes from a search summary and from an OpenAI repo example (https://github.com/openai/openai-agents-python/pull/1993/files , VERIFIED):
  - Origination URI: `sip:proj_<project_id>@sip.api.openai.com;transport=tls`.
  - A project webhook receives `realtime.call.incoming` with a `call_id`.
  - Your server calls an accept endpoint with the model and instructions, then opens `wss://api.openai.com/v1/realtime?call_id=...`.
  - Reject, hang up and refer endpoints exist.
  - Status: PARTIAL. The behaviour is confirmed by the example repo. The official prose is UNVERIFIED.
- **Tools: yes, as your own code.** In that example the agent tools (`faq_lookup_tool`, `update_customer_record`) are functions in the developer's server. The tester writes and hosts them, so the sink is fully controlled. VERIFIED for the example. How tool schemas are set in the session is UNVERIFIED.
- **Terms: not retrieved.** https://openai.com/policies/terms-of-use/ and https://openai.com/policies/row-terms-of-use/ and service-terms all returned 403. I make no statement about their content. UNVERIFIED.

## Twilio ConversationRelay

- **What it is. VERIFIED.** https://www.twilio.com/docs/voice/twiml/connect/conversationrelay : a TwiML noun under `<Connect>` that routes a call to the service. The service does speech-to-text and text-to-speech and connects "to your AI application through a WebSocket". It is not an agent. The LLM and the tools live in the tester's WebSocket server.
- **Inbound SIP: Twilio yes, the combination not stated.** https://www.twilio.com/docs/voice/api/sip-twiml.md : for SIP calls Twilio requests TwiML from your application and "begins executing the instructions contained within the TwiML". A SIP Domain call can therefore, in principle, return `<Connect><ConversationRelay>`. No Twilio page I read says so. UNVERIFIED. The ConversationRelay pages never mention SIP.
- **Tools: fully yours.** The WebSocket server decides everything. No webhook format to match.
- **Terms.**
  - Terms of Service, https://www.twilio.com/en-us/legal/tos , "Last Updated: July 16, 2026": no penetration testing or vulnerability clause found. It points to the Acceptable Use Policy. VERIFIED by keyword search of the page text.
  - AUP, https://www.twilio.com/en-us/legal/aup , "Last Updated: May 12, 2025": under "No Service Integrity Violations", "Finding security vulnerabilities to exploit the Services" and "attempting to bypass any security mechanism or filtering capabilities". It says nothing about consented testing of your own application. VERIFIED.

## Bland AI

- **Inbound SIP: yes. VERIFIED.** https://docs.bland.ai/enterprise-features/SIP-integration : attach your numbers to a SIP trunk with the inbound direction, via the wizard or `POST /v1/sip/attach`. Endpoints such as `us2.sip.bland.ai:5061;transport=tls`. "SIP runs only over TLS on TCP 5061." Signalling and media IPs are listed per region, with RTP on UDP 16384 to 32768. The URL path says "enterprise-features". The page does not say which plan is needed.
- **Tools as webhooks: yes. VERIFIED.** https://docs.bland.ai/tutorials/custom-tools : custom tools "allow your agent to interact with any web API mid-call". The example sends a POST to a developer URL with an `Authorization` header and a body built from agent inputs.
- **Terms. VERIFIED.** https://www.bland.ai/terms (the page headed "Terms of Service", "Last updated: August 28, 2026"). The prohibited-use list includes "Interference with service functionality or security features". No penetration testing or reverse-engineering clause found. The `/terms-of-service` path and the docs `overview` path I guessed returned 404 or the same page.

## What this means for the SIP connector

1. **Do not claim "pointed at real products" yet.** None of this was run. I only read docs.
2. **TLS and SRTP gap.** The connector is UDP only (`sip.go`, lines 66 and 209). Bland is TLS-only. OpenAI's documented URI uses `transport=tls`. ElevenLabs' UDP is "experimental and for testing only". Retell and Vapi did not state transport on the pages I read. A bridge is needed, or TLS and SRTP support in the connector. SRTP is not covered by any page I read. Check each platform's media requirements.
3. **Cheapest real target.** Retell (Register Phone Call API gives a one-time SIP URI; webhook tools are signed) or Vapi (a SIP URI that needs no registration). Both need a first test of UDP acceptance.
4. **Terms.** No platform allows security testing in words. Vapi bans "test any vulnerabilities of the Services". Twilio's AUP bans "attempting to bypass any security mechanism or filtering capabilities". Before publishing results on named products, send each vendor a written notice and ask for consent. The talk can use reference agents (as the draft does) and avoid naming products.
5. **Webhook tools make the sink easy.** Vapi, Retell, ElevenLabs and Bland call a URL the tester hosts, so the nonce-bearing sink the draft needs is a plain HTTP endpoint. Retell blocks private IPs, so the endpoint must be publicly reachable through a tunnel.
