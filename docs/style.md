# Style Guide

Conventions for knowledge-base entries and documentation, so the corpus reads as one
consistent reference.

## Tone

- Formal and precise. No emoji anywhere in the repository.
- Write for a defender testing their own agent. Pair every attack with detection and
  mitigation.
- Prefer concrete predicates over adjectives. "Opens a valve for more than 120
  minutes" is a success condition; "behaves unsafely" is not.

## Entry structure

- Front matter first, following `kb/_template/technique.md`, then the body.
- Body sections in this order: Summary (a linked table of contents), Threat model,
  Payload / encoding, Delivery / transport, Success condition (oracle), Detection and
  mitigation, Reproduction cases, References.
- One technique per file. Group files by channel folder.

## Identifiers

- **slug** — lowercase, hyphenated, encoding channel and primary intent, for example
  `C2-I3-smartfarm-note-override`. Equals the filename.
- **registry_id** — `CW-NNNN`, assigned on merge, never reused, and safe to cite or
  link from a Crosswire run bundle.

## References

- Every entry ends with a dated References section.
- Cite standards by their stable identifier (for example, OWASP LLM01) and add the id
  to `mappings/` before using it in front matter.

## Sanitization

- Placeholder domains, documentation IP ranges, and obvious placeholder credentials
  only. No real personal data. See CONTRIBUTING.md for the full rules.

## Line length

- Wrap prose at 100 columns. Tables and code blocks are exempt (see
  `.markdownlint.json`).
