# C2 — Data-Field Injection

Instructions smuggled into structured fields that the agent parses as text: JSON
label or note fields, device names, tags, and metadata. The agent reads the field as
trusted context rather than untrusted observation.

## Techniques

| registry_id | Technique | Intent | Status |
| --- | --- | --- | --- |
| CW-0001 | [Smart-Farm Irrigation Note-Field Override](C2-I3-smartfarm-note-override.md) | I3, I2 | reproduced |
| CW-0009 | [Web-App Prompt Injection (running-target)](C2-I4-webapp-prompt-injection.md) | I4, I1 | reproduced |

Contributions welcome. Copy [`../_template/technique.md`](../_template/technique.md)
and open a single-file pull request.
