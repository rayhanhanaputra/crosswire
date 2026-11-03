<!-- Thank you for contributing to Crosswire. -->

## What does this pull request add or change?

<!-- One or two sentences. -->

## Type

- [ ] New knowledge-base technique
- [ ] Change to an existing technique
- [ ] Crosswire code (adapter, transport, evidence rule, UI, or test)
- [ ] Documentation or tooling

## Checklist for a knowledge-base technique

- [ ] Copied from `kb/_template/technique.md` into the correct channel folder.
- [ ] Filename equals the `id` slug.
- [ ] `registry_id` left as `CW-XXXX` for a maintainer to assign.
- [ ] `success` is a falsifiable predicate over a privileged action (not vague, not
      LLM-judged).
- [ ] All `mappings` and `tags` ids already exist under `mappings/`.
- [ ] Reproduction cases use an authorized lab target and include a benign control.
- [ ] A `reproduced` status cites a run bundle under `kb/bundles/`.
- [ ] Values are sanitized (placeholder domains, addresses, and credentials only).

## Validation

- [ ] `make check` passes locally.
- [ ] `go vet ./...` and `go test ./...` pass, for a code change.

## Authorization

- [ ] This contribution is not derived from unauthorized access to any third-party
      system, and contains no real personal data.
