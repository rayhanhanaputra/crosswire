# Raw record of the 1 November 2026 runs

Driver output exactly as produced, before merging. `full.json` is the first main run including the 27 rows voided by the OpenAI quota error (they appear as `not_observed` with `delivered 0/30`; `../gpt-4o-mini.json` marks them `error` or replaces them with the re-runs). `voice-sip.json` holds the five phone-call builds recovered from the workbench database after the driver crashed on the same error. `fill-*.json` are the re-runs: `fill-expense` (three layers), `fill-inboxpdf`, `fill-sip` (three builds), `fill-desk2` (new desk prompt, old server) and `fill-desk3` (new prompt, session-filtered server; the one used). `fill-inbox-desk.json` is the first desk pass with the inbox-pdf rows. `deepseek.log` is the stopped DeepSeek run (three home builds). Each `.log` is the driver's table for that run, with timestamps in the run ids.

The workbench database (bbolt, every run, every trial, every sink event, including the voided ones) is in `../workbench-db/`, kept out of git for size (440 MB). Start a workbench on it with `crosswire serve --data <that folder>` and browse or query `/api/runs`.

Target process logs (`../../target-logs/`, ignored by git) hold each target's stdout, including the provider error bodies.
