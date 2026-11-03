---
id:          Cx-Iy-scenario-shortname          # slug; must equal the filename without .md
registry_id: CW-XXXX                           # assigned by a maintainer on merge; never reuse
title:       Human-readable technique title
channel:     Cx                                 # one of C1..C8 (see docs/taxonomy.md)
intent:      [Iy]                               # one or more of I1..I6
effect:      cyber-physical                     # cyber-physical | digital
severity:    medium                             # low | medium | high | critical
scenario:    Deployment context in one line
preconditions: Attacker position and required access, in one or two sentences.
success: >
  The falsifiable condition that defines a successful attack, stated as a predicate
  over a privileged action in the target's action log. This field is mandatory.
mappings:                                       # at least one non-empty mapping required
  owasp-llm:        []                          # ids from mappings/owasp-llm.yaml (2025)
  owasp-agentic:    []                          # ids from mappings/owasp-agentic.yaml
  mitre-atlas:      []                          # ids from mappings/mitre-atlas.yaml
  mitre-attack-ics: []                          # ids from mappings/mitre-attack-ics.yaml (cyber-physical)
tags:        []                                 # ids from mappings/tags.yaml
status:      documented                         # documented | reproduced
# bundle:    kb/bundles/CW-XXXX/               # required when status is reproduced
added:       YYYY-MM-DD
updated:     YYYY-MM-DD
references:
  - "Author, Title, https://…, Month DD, YYYY"
---

# Title  (CW-XXXX · Cx-Iy)

## Summary

- [Threat model](#threat-model)
- [Payload / encoding](#payload--encoding)
- [Delivery / transport](#delivery--transport)
- [Success condition (oracle)](#success-condition-oracle)
- [Detection and mitigation](#detection-and-mitigation)
- [Reproduction cases](#reproduction-cases)
- [References](#references)

## Threat model

Attacker position, trust boundary under test, and the protected asset.

## Payload / encoding

The concrete payload and how it is encoded into the channel. Use placeholder
attacker-controlled values only (see the sanitization rules in CONTRIBUTING.md).

## Delivery / transport

How the encoded payload reaches the agent, and the benign twin used as the
false-positive control.

## Success condition (oracle)

The mechanical predicate over the target's action log that decides success. Do not
rely on an LLM judgment for the primary verdict.

## Detection and mitigation

Detection signals and concrete defenses, framed for a defender testing their own
agent.

## Reproduction cases

| Case | Variant | Expected verdict |
| --- | --- | --- |
| (none yet) | | |

## References

- Author, Title, https://…, Month DD, YYYY
