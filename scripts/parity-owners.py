#!/usr/bin/env python3
"""Who owns a parity row — by path rule, not by a frozen list.

The stub generator used to carry a dict keyed on exact Trakt paths. Renaming the
inventory onto the contract's templates (`/calendars/all/dvd/...` ->
`/calendars/{target}/dvd/...`) left seven rows with no owner, and they were
attributed to #4 as a fallback, which is a lie about who is doing the work.

Three consumers, one source of truth:

    python3 scripts/parity-owners.py            # per-issue counts
    python3 scripts/parity-owners.py --tables   # the markdown table each issue embeds
    python3 scripts/parity-owners.py --unowned  # rows no rule claims (must be empty)

`parity-stubs.py` and `parity-scope.py` import from here, so the stub's filename,
the stub's comment, the per-issue table and the frozen scope cannot disagree.

A *group* is the unit of work; an *issue* is where that group is tracked. They
are the same string for a group that is already filed (``"66"``), and the issue
is looked up in ``GROUP_NAME`` for the groups created when the scope was frozen
(``"PROFILE"`` -> #103). `group_for()` returns the group, `owner_for()` the issue
number, which is what a stub file is named after.
"""
import re
import sys
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parent.parent
MANIFEST = ROOT / "docs/parity/parity.yaml"
TRACKER = 4

# Statuses that mean "work remains" for a group issue. `implemented` rows need no
# owner, and a re-affirmed `decision:` row is out by the owner's call.
OPEN_STATUSES = ("blocked-engine", "blocked-data", "divergent", "out-of-scope")

# Ordered: first match wins. Keyed on the contract's templates, so a reshape
# cannot strand a row the way an exact-path dict did.
RULES = [
    # -- the owner's carve-out: the recommendation/related engine stays 501 ----
    (r"related|^/recommendations/", "RECO"),

    # -- per-section hidden item lists (before the profile reads) ------------
    (r"^/users/hidden/", "HIDDEN"),
    # -- follow requests: the optional shared social server ------------------
    (r"^/users/requests/", "FOLLOW"),
    # -- list management: CRUD, items, collaborations, smart lists -----------
    (r"^/users/\{id\}/(smart-)?lists|^/users/\{id\}/lists/|^/lists/|^/smart-lists/", "LISTS_CRUD"),
    # -- comment moderation, reactions, and the note's subject read ----------
    (r"^/comments/|^/notes/", "COMMENTS"),
    # -- season/episode-level sync -------------------------------------------
    (r"^/sync/collection/episodes", "71"),
    (r"^/sync/", "SYNC"),
    # -- statically-served reference enums -----------------------------------
    (r"^/certifications/", "REFERENCE"),
    # -- period rollups, cross-type feeds, windowed "hot", live activity -----
    (r"^/media/(trending|popular)|"
     r"^/movies/(watched|played|collected|hot|anticipated|favorited)/|"
     r"^/movies/(hot|boxoffice)|"
     r"^/shows/(watched|played|collected|hot|anticipated|favorited)/|"
     r"^/shows/hot|"
     r"/\{id\}/watching|\{id\}/progress/watched/", "66"),
    # -- per-item stats over data we already store ---------------------------
    (r"/\{id\}/stats|/episodes/\{episode\}/(ratings|stats)", "86"),
    # -- release/provider/calendar data --------------------------------------
    (r"^/calendars/|^/watchnow/|^/movies/streaming/|^/shows/streaming/", "69"),
    # -- search ---------------------------------------------------------------
    (r"^/search/", "70"),
    # -- per-item lists reads (lists engine) ---------------------------------
    (r"/seasons/\{season\}/lists/\{type\}|\{id\}/lists/\{type\}", "67"),
    # -- TMDB metadata ingest: videos, studios, credits, certifications,
    #    releases, translations, watch-provider links, person surfaces -------
    (r"/\{id\}/(videos|studios|people|translations|certifications|releases|sentiments|"
     r"watchnow|refresh)|^/shows/\{id\}/seasons/|"
     r"^/people/|^/episodes/|^/seasons/", "68"),
    # -- the public-profile read surface -------------------------------------
    (r"^/users/", "PROFILE"),
]

# group -> (issue number, human name). A group whose issue is not filed yet has
# `None`; the frozen scope refuses to be written while that is true.
GROUP_NAME = {
    "RECO": (49, "related / recommendations (owner carve-out: stays 501)"),
    "66": (66, "stats and rankings"),
    "67": (67, "lists engine — per-item lists"),
    "68": (68, "TMDB metadata ingest"),
    "69": (69, "calendars and Watch Now data"),
    "70": (70, "search engine"),
    "71": (71, "season/episode-level sync"),
    "86": (86, "per-item reads over stored data"),
    "PROFILE": (103, "public-profile read surface"),
    "DIVERGENT": (104, "divergent shapes — serve the contract path"),
    "LISTS_CRUD": (105, "list management — writes, items, smart lists"),
    "SYNC": (106, "remaining sync reads and writes"),
    "COMMENTS": (107, "comment moderation, reactions and the note item read"),
    "HIDDEN": (108, "per-section hidden item lists"),
    "REFERENCE": (109, "reference-data certification enums"),
    "FOLLOW": (110, "follow requests (blocked on #54)"),
}


def group_for(trakt, status=None):
    # A divergent row is a shape fix, not a feature: the data is served, only the
    # path or the params differ. They are one cross-cutting slice (the same thin
    # alias route plus nested-id resolution, all touching the shared registry), so
    # they go to their own group rather than being scattered across the engines.
    if status == "divergent":
        return "DIVERGENT"
    path = trakt.split(" ", 1)[1] if " " in trakt else trakt
    for pat, group in RULES:
        if re.search(pat, path):
            return group
    return None


def owner_for(trakt, status=None):
    """The issue a row is tracked on — a stub file is named `stub_<this>.go`."""
    group = group_for(trakt, status)
    if group is None:
        return None
    return GROUP_NAME.get(group, (None,))[0] or group


def _load():
    doc = yaml.safe_load(MANIFEST.read_text())
    for sec in doc["sections"]:
        for r in sec["rows"] or []:
            if not (r.get("trakt") or "").strip():
                continue
            yield sec["name"], r


def _row(section, r):
    t = (r.get("trakt") or "").strip()
    group = group_for(t, r["status"])
    return {"trakt": t, "section": section, "status": r["status"],
            "ours": r.get("ours") or "",
            "blocker": " ".join(str(r.get("engine") or r.get("needs") or "").split()),
            "compat": " ".join(str(r.get("compat") or "").split()),
            "rationale": " ".join(str(r.get("rationale") or "").split()),
            "group": group,
            "owner": GROUP_NAME.get(group, (None,))[0] or group}


def rows():
    """Every row with work left in it."""
    out = []
    for section, r in _load():
        if r["status"] == "implemented":
            continue
        if r["status"] == "out-of-scope" and not (r.get("rationale") or "").strip().startswith("deferred:"):
            # a re-affirmed `decision:` row is out by the owner's call, not a maybe
            continue
        out.append(_row(section, r))
    return out


def rows_all():
    """Every row with a Trakt path, whatever its grade — for the scope document."""
    return [_row(section, r) for section, r in _load()]


def table(group):
    mine = [r for r in rows() if r["group"] == group]
    if not mine:
        return None
    lines = ["## The rows this closes", "",
             "| Trakt path | Section | Status | Note |", "|---|---|---|---|"]
    for r in sorted(mine, key=lambda x: (x["status"], x["trakt"])):
        note = r["blocker"] or r["compat"] or r["rationale"].split(":", 1)[-1].strip()
        note = note.replace("|", "\\|")
        lines.append(f"| `{r['trakt']}` | {r['section']} | {r['status']} | {note} |")
    lines.append("")
    lines.append(f"{len(mine)} row(s). Generated from `docs/parity/parity.yaml` by "
                 "`scripts/parity-owners.py --tables`; do not hand-edit.")
    return "\n".join(lines)


def main():
    all_rows = rows()
    if "--tables" in sys.argv:
        for group in GROUP_NAME:
            t = table(group)
            if t:
                print(f"===GROUP:{group}\n{t}\n===END")
        return
    if "--unowned" in sys.argv:
        for r in all_rows:
            if r["group"] is None:
                print(f"  {r['status']:14s} {r['trakt']}")
        n = sum(1 for r in all_rows if r["group"] is None)
        print(f"\n{n} unowned row(s) of {len(all_rows)}")
        return
    counts = [(g, sum(1 for r in all_rows if r["group"] == g)) for g in GROUP_NAME]
    unowned = sum(1 for r in all_rows if r["group"] is None)
    for group, n in sorted(counts, key=lambda kv: (GROUP_NAME[kv[0]][0] is None,
                                                   GROUP_NAME[kv[0]][0] or 0)):
        issue, name = GROUP_NAME[group]
        print(f"  #{issue if issue else '?'} ({name}): {n}")
    print(f"\n  unowned -> #4: {unowned}")
    print(f"  total open rows: {len(all_rows)}")


if __name__ == "__main__":
    main()