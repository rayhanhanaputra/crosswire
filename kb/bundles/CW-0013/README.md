# Run bundle for CW-0013

Exported from Crosswire (`GET /api/runs/<id>/bundle`) on 2026-10-31.
Target: reference `camera-agent`, policy vulnerable, chat guardrail on, model
`gpt-4o-mini`, temperature 0. N = 10 per arm, concurrency 2.

Kept here: `run.json` (every trial, transcript, findings and verdict),
`sink_events.json`, `manifest.sha256.json` (hashes of all 20 artifacts), and one
sample artifact (`artifacts/injected-0.png`). The other 19 PNGs were removed to keep
the repository small; their SHA-256 values remain in the manifest, and every trial's
transcript is in `run.json`.
