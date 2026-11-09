#!/usr/bin/env python3
"""Who owns a blocked parity row — by path rule, not by a frozen list.

The stub generator used to carry a dict keyed on exact Trakt paths. Renaming the
inventory onto the contract's templates (`/calendars/all/dvd/...` ->
`/calendars/{target}/dvd/...`) left seven rows with no owner, and they were
attributed to #4 as a fallback, which is a lie about who is doing the work.

Two consumers, one source of truth:

    python3 scripts/parity-owners.py            # per-issue counts
    python3 scripts/parity-owners.py --tables   # the markdown table each issue embeds

`parity-stubs.py` imports `owner_for` so the stub's comment names the real issue.
"""
import re
import sys
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parent.parent
MANIFEST = ROOT / "docs/parity/parity.yaml"
TRACKER = 4

# Ordered: first match wins. Keyed on the contract's templates, so a reshape
# cannot strand a row the way an exact-path dict did.
RULES = [
    (r"^/lists/|/\{id\}/lists/?$|\{id\}/lists/\{", 67),          # list management
    (r"\{id\}/stats", 86),                                       # per-item stats reads
    (r"^/media/|/\{id\}/watching|\{id\}/progress/watched|"
     r"^/movies/(watched|played|collected|hot|anticipated)/|^/movies/(hot|boxoffice)|"
     r"^/shows/(watched|played|collected|hot|anticipated)/|^/shows/hot", 66),   # rankings
    (r"^/calendars/|^/watchnow/|^/movies/streaming/|^/shows/streaming/", 69),
    (r"^/search/", 70),
    (r"^/sync/(collection|watchlist|ratings|favorites|history)", 71),
    (r"related|^/recommendations/", 49),
    (r"^/movies/\{id\}/|^/shows/\{id\}/|^/people/|^/episodes/|^/seasons/|^/certifications/", 68),
]

GROUP_NAME = {
    49: "related / recommendations",
    66: "stats and rankings",
    67: "lists",
    68: "metadata ingest",
    69: "calendars and Watch Now",
    70: "search",
    71: "sync season/episode",
    86: "per-item reads",
}


def owner_for(trakt):
    path = trakt.split(" ", 1)[1] if " " in trakt else trakt
    for pat, issue in RULES:
        if re.search(pat, path):
            return issue
    return None


def rows():
    doc = yaml.safe_load(MANIFEST.read_text())
    out = []
    for sec in doc["sections"]:
        for r in sec["rows"] or []:
            t = (r.get("trakt") or "").strip()
            if not t or r["status"] not in ("blocked-engine", "blocked-data"):
                continue
            blocker = " ".join(str(r.get("engine") or r.get("needs") or "").split()) or "—"
            out.append({"trakt": t, "section": sec["name"], "status": r["status"],
                        "blocker": blocker, "owner": owner_for(t)})
    return out


def table(issue):
    mine = [r for r in rows() if r["owner"] == issue]
    if not mine:
        return None
    lines = ["## The rows this closes", "",
             "| Trakt path | Section | Status | Blocker |", "|---|---|---|---|"]
    for r in sorted(mine, key=lambda x: (x["section"], x["trakt"])):
        lines.append(f"| `{r['trakt']}` | {r['section']} | {r['status']} | {r['blocker']} |")
    lines.append("")
    lines.append(f"{len(mine)} row(s). Generated from `docs/parity/parity.yaml` by "
                 "`scripts/parity-owners.py --tables`; do not hand-edit.")
    return "\n".join(lines)


def main():
    all_rows = rows()
    if "--tables" in sys.argv:
        for issue in sorted(GROUP_NAME):
            t = table(issue)
            if t:
                print(f"===ISSUE:{issue}\n{t}\n===END")
        return
    unowned = [r for r in all_rows if r["owner"] is None]
    counts = [(i, sum(1 for r in all_rows if r["owner"] == i)) for i in GROUP_NAME]
    counts.append((None, len(unowned)))
    for issue, n in sorted(counts, key=lambda kv: (kv[0] is None, kv[0] or 0)):
        print(f"  #{issue if issue else TRACKER} ({GROUP_NAME.get(issue, 'tracker')}): {n}")
    if unowned:
        print("\nunowned -> #4:")
        for r in unowned:
            print("   ", r["trakt"])


if __name__ == "__main__":
    main()