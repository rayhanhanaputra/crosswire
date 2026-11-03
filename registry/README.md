# Registry (generated)

This directory is the machine-readable spine of the corpus. Every file here is
**generated** from the front matter of the knowledge-base entries by
`tools/build_registry.py`. Do not edit these files by hand; edits are overwritten and
continuous integration rebuilds them on every change.

| File | Purpose |
| --- | --- |
| `index.yaml` | Full record per technique: registry id, slug, channel, intent, mappings, and status. |
| `index.csv` | Flat table for spreadsheets and paper tables. |
| `matrix.md` | The Modality Injection Matrix (channel by intent) with links to entries. |
| `stats.json` | Counts by channel, intent, status, and tag. |

## Rebuild

```bash
python tools/build_registry.py            # regenerate all registry files
python tools/build_registry.py --check    # validate only; non-zero exit on any error
```

Validation enforced by the builder:

- Required front-matter fields are present, including a non-empty `success`.
- `id` equals the entry filename.
- `registry_id` is unique and matches the pattern `CW-NNNN`.
- Every `mappings` id exists in the corresponding file under `mappings/`.
- Every `tags` id exists in `mappings/tags.yaml`.
