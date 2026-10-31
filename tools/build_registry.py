#!/usr/bin/env python3
"""Build and validate the AIAgentsAllTheThings registry from knowledge-base entries.

The knowledge base (kb/) is the source of truth. This script reads each technique's
YAML front matter, validates it against the controlled vocabularies in mappings/, and
regenerates the machine-readable index under registry/.

Usage:
    python tools/build_registry.py            # regenerate registry/ files
    python tools/build_registry.py --check    # validate only; non-zero exit on error
"""
from __future__ import annotations

import argparse
import csv
import io
import json
import re
import sys
from pathlib import Path

try:
    import yaml
except ImportError:  # pragma: no cover
    sys.exit("PyYAML is required: pip install -r tools/requirements.txt")

ROOT = Path(__file__).resolve().parent.parent
KB = ROOT / "kb"
MAPPINGS = ROOT / "mappings"
REGISTRY = ROOT / "registry"

CHANNELS = ["C1", "C2", "C3", "C4", "C5", "C6", "C7", "C8"]
INTENTS = ["I1", "I2", "I3", "I4", "I5", "I6"]
STATUSES = ["documented", "reproduced"]
SEVERITIES = ["low", "medium", "high", "critical"]
EFFECTS = ["cyber-physical", "digital"]
REQUIRED = ["id", "registry_id", "title", "channel", "intent", "effect",
            "severity", "scenario", "success", "status", "added", "references"]
REGISTRY_ID_RE = re.compile(r"^AAT-\d{4}$")

CHANNEL_LABELS = {
    "C1": "Sensor value", "C2": "Data field", "C3": "Actuator state",
    "C4": "Voice / DTMF", "C5": "Optical / QR", "C6": "RF / protocol",
    "C7": "Indirect / inter-agent", "C8": "Feedback-loop",
}
INTENT_LABELS = {
    "I1": "Override", "I2": "Tool abuse", "I3": "Safety-limit",
    "I4": "Exfiltration", "I5": "DoS", "I6": "Persistence",
}


def load_vocab(name: str) -> set[str]:
    path = MAPPINGS / name
    if not path.exists():
        return set()
    data = yaml.safe_load(path.read_text()) or {}
    return set((data.get("ids") or {}).keys())


def parse_front_matter(path: Path) -> dict:
    text = path.read_text()
    if not text.startswith("---"):
        raise ValueError(f"{path}: missing YAML front matter")
    parts = text.split("---", 2)
    if len(parts) < 3:
        raise ValueError(f"{path}: malformed front matter delimiters")
    return yaml.safe_load(parts[1]) or {}


def entry_files() -> list[Path]:
    files = []
    for path in KB.rglob("*.md"):
        if path.name == "README.md" or "_template" in path.parts:
            continue
        files.append(path)
    return sorted(files)


def validate(fm: dict, path: Path, vocab: dict,
             seen_ids: dict, errors: list[str]) -> None:
    rel = path.relative_to(ROOT)
    for field in REQUIRED:
        if field not in fm or fm[field] in (None, "", []):
            errors.append(f"{rel}: missing required field '{field}'")
    if not str(fm.get("success", "")).strip():
        errors.append(f"{rel}: 'success' must be a non-empty falsifiable condition")
    if fm.get("id") != path.stem:
        errors.append(f"{rel}: id '{fm.get('id')}' does not match filename '{path.stem}'")
    rid = fm.get("registry_id", "")
    if not REGISTRY_ID_RE.match(str(rid)):
        errors.append(f"{rel}: registry_id '{rid}' must match AAT-NNNN")
    elif rid in seen_ids:
        errors.append(f"{rel}: registry_id '{rid}' also used by {seen_ids[rid]}")
    else:
        seen_ids[rid] = str(rel)
    if fm.get("channel") not in CHANNELS:
        errors.append(f"{rel}: channel '{fm.get('channel')}' not in {CHANNELS}")
    for i in fm.get("intent", []):
        if i not in INTENTS:
            errors.append(f"{rel}: intent '{i}' not in {INTENTS}")
    if fm.get("status") not in STATUSES:
        errors.append(f"{rel}: status '{fm.get('status')}' not in {STATUSES}")
    if fm.get("severity") not in SEVERITIES:
        errors.append(f"{rel}: severity '{fm.get('severity')}' not in {SEVERITIES}")
    if fm.get("effect") not in EFFECTS:
        errors.append(f"{rel}: effect '{fm.get('effect')}' not in {EFFECTS}")
    for scheme in ("owasp-llm", "owasp-agentic", "mitre-atlas", "mitre-attack-ics"):
        for mid in (fm.get("mappings") or {}).get(scheme, []):
            if mid not in vocab[scheme]:
                errors.append(f"{rel}: mapping id '{mid}' not in mappings/{scheme}.yaml")
    for tag in fm.get("tags", []):
        if tag not in vocab["tags"]:
            errors.append(f"{rel}: tag '{tag}' not in mappings/tags.yaml")
    mappings = fm.get("mappings") or {}
    if not any(mappings.get(s) for s in ("owasp-llm", "owasp-agentic", "mitre-atlas", "mitre-attack-ics")):
        errors.append(f"{rel}: at least one non-empty framework mapping is required")
    if fm.get("status") == "reproduced":
        validate_bundle(fm, rel, errors)


def validate_bundle(fm: dict, rel: Path, errors: list[str]) -> None:
    """A 'reproduced' claim must point at a stored Crosswire run bundle.

    The bundle is the evidence: run.json with a verdict of confirmed or observed,
    produced through a real (not modeled) channel. Front matter alone proves nothing;
    before this check every entry claimed 'reproduced' while nothing could rerun it.
    """
    bundle = fm.get("bundle")
    if not bundle:
        errors.append(f"{rel}: status 'reproduced' requires a 'bundle' path under kb/bundles/")
        return
    path = ROOT / str(bundle)
    run_json = path / "run.json" if path.is_dir() else None
    if run_json is None or not run_json.exists():
        errors.append(f"{rel}: bundle '{bundle}' has no run.json")
        return
    try:
        run = json.loads(run_json.read_text())
    except json.JSONDecodeError as exc:
        errors.append(f"{rel}: bundle run.json is not valid JSON ({exc})")
        return
    outcome = (run.get("verdict") or {}).get("outcome")
    if outcome not in ("confirmed", "observed"):
        errors.append(f"{rel}: bundle verdict is '{outcome}', not confirmed/observed")
    if (run.get("channel") or {}).get("modeled"):
        errors.append(f"{rel}: bundle used a modeled channel; modeled runs cannot reproduce an entry")


def build() -> tuple[list[dict], list[str]]:
    vocab = {
        "owasp-llm": load_vocab("owasp-llm.yaml"),
        "owasp-agentic": load_vocab("owasp-agentic.yaml"),
        "mitre-atlas": load_vocab("mitre-atlas.yaml"),
        "mitre-attack-ics": load_vocab("mitre-attack-ics.yaml"),
        "tags": load_vocab("tags.yaml"),
    }
    errors: list[str] = []
    seen_ids: dict = {}
    records: list[dict] = []
    for path in entry_files():
        try:
            fm = parse_front_matter(path)
        except ValueError as exc:
            errors.append(str(exc))
            continue
        validate(fm, path, vocab, seen_ids, errors)
        records.append({
            "registry_id": fm.get("registry_id"),
            "id": fm.get("id"),
            "title": fm.get("title"),
            "channel": fm.get("channel"),
            "intent": fm.get("intent", []),
            "effect": fm.get("effect"),
            "severity": fm.get("severity"),
            "status": fm.get("status"),
            "bundle": fm.get("bundle"),
            "path": str(path.relative_to(ROOT)),
            "mappings": fm.get("mappings", {}),
            "tags": fm.get("tags", []),
        })
    records.sort(key=lambda r: str(r["registry_id"]))
    return records, errors


def write_outputs(records: list[dict]) -> None:
    REGISTRY.mkdir(exist_ok=True)
    header = "# GENERATED by tools/build_registry.py — do not edit by hand.\n"
    (REGISTRY / "index.yaml").write_text(
        header + yaml.safe_dump({"techniques": records}, sort_keys=False))

    buf = io.StringIO()
    writer = csv.writer(buf)
    writer.writerow(["registry_id", "id", "title", "channel", "intent",
                     "effect", "severity", "status", "path"])
    for r in records:
        writer.writerow([r["registry_id"], r["id"], r["title"], r["channel"],
                         "|".join(r["intent"]), r["effect"], r["severity"],
                         r["status"], r["path"]])
    (REGISTRY / "index.csv").write_text(buf.getvalue())

    write_matrix(records)
    write_stats(records)


def write_matrix(records: list[dict]) -> None:
    grid = {c: {i: [] for i in INTENTS} for c in CHANNELS}
    for r in records:
        for i in r["intent"]:
            link = f"[{r['registry_id']}](../{r['path']})"
            grid[r["channel"]][i].append(link)
    lines = [
        "<!-- GENERATED by tools/build_registry.py — do not edit by hand. -->",
        "# Modality Injection Matrix",
        "",
        "Rows are channels (C1..C8), columns are adversary intents (I1..I6). Empty "
        "cells are open contribution targets.",
        "",
        "| Channel \\ Intent | " + " | ".join(
            f"{i} {INTENT_LABELS[i]}" for i in INTENTS) + " |",
        "| --- | " + " | ".join("---" for _ in INTENTS) + " |",
    ]
    for c in CHANNELS:
        cells = [" ".join(grid[c][i]) for i in INTENTS]
        lines.append(f"| {c} {CHANNEL_LABELS[c]} | " + " | ".join(cells) + " |")
    channels_used = len({r["channel"] for r in records})
    lines += ["", f"Counts: {len(records)} technique(s) across {channels_used} "
                  f"channel(s). See `stats.json` for the full breakdown.", ""]
    (REGISTRY / "matrix.md").write_text("\n".join(lines))


def write_stats(records: list[dict]) -> None:
    by_tag: dict[str, int] = {}
    stats = {
        "_generated_by": "tools/build_registry.py",
        "total": len(records),
        "by_channel": {c: 0 for c in CHANNELS},
        "by_intent": {i: 0 for i in INTENTS},
        "by_status": {s: 0 for s in STATUSES},
        "by_severity": {s: 0 for s in SEVERITIES},
        "by_tag": by_tag,
    }
    for r in records:
        stats["by_channel"][r["channel"]] += 1
        for i in r["intent"]:
            stats["by_intent"][i] += 1
        if r["status"] in stats["by_status"]:
            stats["by_status"][r["status"]] += 1
        if r["severity"] in stats["by_severity"]:
            stats["by_severity"][r["severity"]] += 1
        for tag in r.get("tags", []):
            by_tag[tag] = by_tag.get(tag, 0) + 1
    stats["by_tag"] = dict(sorted(by_tag.items()))
    (REGISTRY / "stats.json").write_text(json.dumps(stats, indent=2) + "\n")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true",
                        help="validate only; do not write registry files")
    args = parser.parse_args()

    records, errors = build()
    if errors:
        print("Validation failed:", file=sys.stderr)
        for e in errors:
            print(f"  - {e}", file=sys.stderr)
        return 1
    if args.check:
        noun = "entry" if len(records) == 1 else "entries"
        print(f"OK: {len(records)} {noun} valid.")
        return 0
    write_outputs(records)
    print(f"Wrote registry for {len(records)} technique(s).")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
