#!/usr/bin/env python3
"""Reconcile docs/parity/parity.yaml against the vendored upstream Trakt contract.

Reads:
  docs/parity/upstream/trakt-openapi.json   (generated from trakt/trakt-api, pinned)
  docs/parity/parity.yaml                   (our graded inventory)

Writes:
  docs/parity/schema-index.json             (per-operation request/response shape)
  docs/parity/reconciliation.md             (row → upstream mapping, and the gaps)

Both outputs are generated: re-run this after bumping the upstream contract.
Exit status is always 0 — this is a measurement, not a gate (see #4: progress is
graded by `cmd/parity`, and this file exists to make the *shape* gap legible).
"""
import json
import re
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parent.parent
SPEC = ROOT / "docs/parity/upstream/trakt-openapi.json"
MANIFEST = ROOT / "docs/parity/parity.yaml"
OUT_INDEX = ROOT / "docs/parity/schema-index.json"
OUT_REPORT = ROOT / "docs/parity/reconciliation.md"


def shape(node):
    """A compact, readable description of a JSON-Schema node."""
    if not isinstance(node, dict):
        return None
    if "allOf" in node:
        return {"allOf": [shape(x) for x in node["allOf"]]}
    if "oneOf" in node or "anyOf" in node:
        key = "oneOf" if "oneOf" in node else "anyOf"
        return {key: [shape(x) for x in node[key]]}
    t = node.get("type")
    if isinstance(t, list):          # the generator emits union types as lists
        t = t[0] if len(t) == 1 else "|".join(str(x) for x in t)
    if t == "array":
        return {"array": shape(node.get("items") or {})}
    if t == "object" or "properties" in node:
        props = node.get("properties") or {}
        return {
            "type": "object",
            "keys": sorted(props),
            "required": sorted(node.get("required") or []),
        }
    if "enum" in node:
        return {"type": "enum", "values": node["enum"]}
    out = {"type": t or "unknown"}
    if node.get("format"):
        out["format"] = node["format"]
    if "nullable" in node:
        out["nullable"] = node["nullable"]
    return out


def load_ops():
    spec = json.loads(SPEC.read_text())
    ops = {}
    for path, item in spec["paths"].items():
        for method, op in item.items():
            if method in ("get", "post", "put", "patch", "delete"):
                ops[f"{method.upper()} {path}"] = op
    return ops


def params_of(op):
    out = []
    for p in op.get("parameters") or []:
        sch = p.get("schema") or {}
        out.append({
            "name": p.get("name"),
            "in": p.get("in"),
            "required": bool(p.get("required")),
            "type": sch.get("type") or ("enum" if "enum" in sch else None),
            "enum": sch.get("enum"),
        })
    return out


def body_of(op):
    rb = op.get("requestBody")
    if not rb:
        return None
    for ct, media in (rb.get("content") or {}).items():
        return {"contentType": ct, "schema": shape(media.get("schema") or {})}
    return None


def responses_of(op):
    out = {}
    for code, r in (op.get("responses") or {}).items():
        for ct, media in (r.get("content") or {}).items():
            out[str(code)] = shape(media.get("schema") or {})
            break
        out.setdefault(str(code), None)
    return out


def norm(path):
    return re.sub(r"\{[^}]+\}", "{}", path)


def main():
    ops = load_ops()
    index = {
        key: {
            "operationId": op.get("operationId"),
            "tags": op.get("tags"),
            "summary": " ".join((op.get("summary") or "").split())[:160],
            "parameters": params_of(op),
            "body": body_of(op),
            "responses": responses_of(op),
        }
        for key, op in ops.items()
    }
    OUT_INDEX.write_text(json.dumps(index, indent=2, sort_keys=True))

    man = yaml.safe_load(MANIFEST.read_text())
    rows = [(s["name"], r) for s in man["sections"] for r in s["rows"] or []]

    by_norm = {}
    for key in ops:
        by_norm.setdefault(norm(key.split(" ", 1)[1]), []).append(key)

    exact, template, unmatched, no_path = [], [], [], []
    for section, r in rows:
        trakt = (r.get("trakt") or "").strip()
        if not trakt:
            no_path.append((section, r))
            continue
        if trakt in ops:
            exact.append((section, r, trakt))
        elif by_norm.get(norm(trakt.split(" ", 1)[1])):
            template.append((section, r, by_norm[norm(trakt.split(" ", 1)[1])][0]))
        else:
            # candidate: same method, longest shared path prefix
            method = trakt.split(" ", 1)[0]
            path = trakt.split(" ", 1)[1]
            best, best_len = None, 0
            for key in ops:
                m, p = key.split(" ", 1)
                if m != method:
                    continue
                shared = 0
                for a, b in zip(p.split("/"), path.split("/")):
                    if a != b:
                        break
                    shared += 1
                if shared > best_len:
                    best, best_len = key, shared
            unmatched.append((section, r, best))

    tracked = {r.get("trakt") for _, r in rows if r.get("trakt")}
    untracked = sorted(k for k in ops if k not in tracked)

    lines = [
        "# Reconciliation: our inventory against the upstream contract",
        "",
        "Generated by `scripts/parity-reconcile.py` from",
        "`docs/parity/upstream/trakt-openapi.json` (Trakt's own contracts) and",
        "`docs/parity/parity.yaml`. Do not edit by hand.",
        "",
        f"- upstream operations: **{len(ops)}**",
        f"- manifest rows with a Trakt path: **{len(exact) + len(template) + len(unmatched)}**",
        f"- exact path match: **{len(exact)}**",
        f"- match after normalising path parameters: **{len(template)}**",
        f"- **no upstream match: {len(unmatched)}**",
        f"- upstream operations with no manifest row: **{len(untracked)}**",
        "",
        "## Rows whose Trakt path is not in the upstream contract",
        "",
        "These are the reason the manifest is an *instance* inventory rather than a",
        "template one: rows such as `/sync/watchlist/movies` or",
        "`/calendars/all/movies/{start_date}/{days}` spell out a parameter value that the",
        "contract keeps as a placeholder (`{type}`, `{target}`). Until the rows are",
        "rewritten to the contract's own templates, a row can be `implemented` while the",
        "template it belongs to is only partly served.",
        "",
        "| our row | status | closest upstream operation |",
        "|---|---|---|",
    ]
    for section, r, cand in unmatched:
        lines.append(f"| `{r['trakt']}` | {r.get('status')} | `{cand}` |" if cand else f"| `{r['trakt']}` | {r.get('status')} | — |")

    lines += [
        "",
        "## Rows that matched only after normalising parameters",
        "",
        "| our row | status | upstream operation |",
        "|---|---|---|",
    ]
    for section, r, cand in template:
        lines.append(f"| `{r['trakt']}` | {r.get('status')} | `{cand}` |")

    groups = {}
    for key in untracked:
        top = key.split(" ", 1)[1].strip("/").split("/")[0]
        groups.setdefault(top, []).append(key)
    lines += [
        "",
        f"## Upstream operations with no manifest row ({len(untracked)})",
        "",
        "Grouped by first path segment. Some of these are deliberately out of scope for",
        "a self-hosted tracker (trakt's `younify`/`smart-lists`/`oauth` surfaces), but",
        "the group list is the honest shape of what '100% parity' means.",
        "",
        "| first segment | operations |",
        "|---|---|",
    ]
    for top, keys in sorted(groups.items(), key=lambda kv: -len(kv[1])):
        lines.append(f"| `/{top}` | {len(keys)} |")
    lines += ["", "<details><summary>full list</summary>", ""]
    for key in untracked:
        lines.append(f"- `{key}`")
    lines += ["", "</details>", ""]

    OUT_REPORT.write_text("\n".join(lines))
    print(f"schema-index.json: {len(index)} operations")
    print(f"reconciliation.md: exact={len(exact)} template={len(template)} "
          f"unmatched={len(unmatched)} untracked={len(untracked)}")


if __name__ == "__main__":
    main()