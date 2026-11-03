# Contributing

Thank you for helping build Crosswire. The tool is a single Go binary: it renders a
payload into the artifact an agent perceives, delivers it over a real transport,
collects the agent's tool calls at an out-of-band sink, and decides a verdict from
repeated trials against a control arm. The knowledge base documents reusable
techniques and mitigations.

Contributions normally improve one of three areas: a channel adapter or transport, the
verdict and evidence rules, or a knowledge-base technique.

## Working on the binary

Go 1.22+, no cgo. There is no separate frontend build: the UI is plain HTML, CSS and
JavaScript under `internal/server/ui/`, embedded with `go:embed`.

```bash
go build -o crosswire ./cmd/crosswire
go test ./...          # every transport against in-process fakes, offline
./crosswire selftest   # the same fakes from the binary, as a table
make check             # validate kb/ against registry/
make lint              # markdownlint, the version CI uses
```

- A **channel adapter** (`internal/channel/`) describes what the agent perceives and
  renders the real artifact. If an adapter only relabels text, mark it `modeled`; a
  modeled channel can never back a `reproduced` claim.
- A **transport** (`internal/connector/`) describes how that artifact reaches the
  target. Keep it channel-agnostic; do not couple a transport to one scenario.
- Every transport needs a fake under `internal/fakes/` and a row in
  `internal/selftest/`, so `go test ./...` and `crosswire selftest` stay offline.
- **Evidence rules** live in `internal/evidence/`. A hit is a forbidden tool that
  executed at the sink carrying the trial's canary. Reply text is a hint, never
  evidence. Changes here need a test that covers the control arm.
- Never store credential values: in a target profile, in the run store, in the UI, or
  in `/health`. Profiles reference environment-variable names only.
- Changes to the wire contract or the verdict rules must also update
  [`docs/CONTRACT.md`](docs/CONTRACT.md), which is the normative document.

## Adding a knowledge-base technique

1. Copy [`kb/_template/technique.md`](kb/_template/technique.md) into the folder for
   its channel, for example `kb/C2-data-field/`.
2. Name the file with its slug, for example `C2-I3-smartfarm-note-override.md`. The
   slug encodes the channel and primary intent.
3. Fill in the front matter and the body sections.
4. Leave `registry_id` as `CW-XXXX`; a maintainer assigns the next number on merge.
5. Open a pull request with just that file (plus images under an `Images/` subfolder
   if needed).

### Mandatory rules

- **A falsifiable `success` field is required.** It must be a predicate over a
  privileged action in the target's action log. An entry whose success is vague,
  subjective, or decided only by an LLM judgment is not accepted. This is the rule
  that separates a technique from a description.
- **Cite only known mapping ids.** Values in `mappings` and `tags` must already exist
  in the files under [`mappings/`](mappings/). To use a new one, add it there in the
  same pull request.
- **A `reproduced` entry must cite a run bundle.** Point `bundle` at a stored bundle
  under `kb/bundles/` whose verdict is `confirmed` or `observed` on a real (not
  modeled) channel. Without a bundle, the status is `documented`.

### Sanitization rules

Use placeholder, non-routable, non-real values only.

- Domains: `example.com`, `example.org`, or `attacker.example`.
- Addresses: documentation ranges (`192.0.2.0/24`, `198.51.100.0/24`,
  `203.0.113.0/24`) and `10.0.0.0/8` for lab networks.
- Credentials: obvious placeholders such as `P@ssw0rd-EXAMPLE`; never real secrets.
- No real personal data, no real PII or PHI, no real customer identifiers, and no data
  from a system you are not authorized to test.

## Before you open a pull request

```bash
make all        # go vet + go test + registry check + markdownlint
```

Or individually:

```bash
go vet ./...
go test ./...
make check                                 # validate the knowledge base
make registry                              # regenerate registry/ if you changed entries
make lint                                  # markdownlint
```

`make check` and `make registry` run `tools/build_registry.py`, the one piece of
Python left in the repository; it needs `pip install -r tools/requirements.txt`.

Continuous integration runs the same checks on Linux, macOS and Windows, builds the
binary for each, and runs `crosswire selftest`. A pull request that fails any of these
will not merge.

## Licensing of contributions

By contributing you agree that your knowledge-base contributions are licensed under
CC BY 4.0 and your code contributions under Apache-2.0, matching the repository's
dual license. See [`LICENSE-DOCS`](LICENSE-DOCS) and [`LICENSE-CODE`](LICENSE-CODE).

Authorship is tracked through git commit history. Do not add author names to entry
front matter or documentation; a normal push records attribution automatically.

## Code of conduct and authorized use

This project is for defenders and researchers testing systems they own or are
explicitly authorized to test. Do not contribute material derived from unauthorized
access to third-party systems. See [`SECURITY.md`](SECURITY.md).
