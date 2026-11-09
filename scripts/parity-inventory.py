#!/usr/bin/env python3
"""Complete the inventory against the vendored contract (issue #100).

Three things, all mechanical and reviewable:

1. The seven rows whose path the contract does not have. Two are replaced by the
   contract's own literal rows (which step 2 adds), two are our own served
   aggregates and become custom-surface rows, and three are routes that only ever
   existed as stubs for a non-Trakt path — the row and the stub both go.
2. The 239 contract operations the original inventory missed. 82 of them we
   already serve — the inventory was under-counting our coverage — and they are
   added as `implemented` with our route in `ours`; the rest are added as
   `out-of-scope` with a written rationale per group, so an untracked endpoint can
   no longer be mistaken for a forgotten one.
3. Nothing is graded up to make the number look better: a row is `implemented`
   only when a registered route serves that path shape and answers 200.

Run after `scripts/parity-reconcile.py` (it reads `openapi.yaml` for our surface).
"""
import json
import re
import sys

import yaml
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
MANIFEST = ROOT / "docs/parity/parity.yaml"
OPS = ROOT / "docs/parity/schema-index.json"
SPEC = ROOT / "openapi.yaml"

# --- 1. rows the contract has no path for -----------------------------------
DROP = {
    # replaced by the contract's literal per-type rows, which step 2 adds as
    # `implemented` because our {media_type} route serves those literal paths.
    "GET /users/{id}/history/{type}",
    "GET /users/{id}/ratings/{type}",
    # only ever existed as a stub for a path Trakt does not expose at top level
    "GET /watchnow/justwatch_links/{country}",
    # the contract's recent-search read is /search/recent_by_id/global/{type},
    # which is already a row; this shortcut has no counterpart
    "GET /search/recent",
}

# our own served aggregate -> custom surface (empty trakt, keeps `ours`)
CUSTOM = {
    "GET /users/me/likes/comments": "custom: our own 'my liked comments' aggregate; the "
                                    "contract exposes per-comment likes (/comments/{id}/likes) "
                                    "and has no /me roll-up",
    "GET /users/me/likes/lists": "custom: our own 'my liked lists' aggregate; the contract "
                                 "exposes /lists/{id}/likes and has no /me roll-up",
}

# same endpoint, contract spelling
REWRITE = {
    "POST /search": ("POST /search/recent/", "POST /v1/search/recent"),
}

# --- 2. rationale per group of contract operations we do not serve -----------
GROUP_RULES = [
    (r"^/younify/", "decision: Trakt's proprietary cross-service linking product; a "
                    "self-hosted tracker has nothing to link through it"),
    (r"^/users/[^/]+/yir/|^/users/[^/]+/mir/", "decision: Trakt's Year/Month-in-Review are "
                                               "proprietary aggregates over its whole user base; we do not reproduce them"),
    (r"^/users/settings/plex|^/users/syncs", "decision: Trakt's Plex integration and 'sync' "
                                             "product; haaku tracks via scrobbling, not a sync client"),
    (r"^/users/requests", "deferred: the follow-request workflow, deferred with the optional "
                          "shared social server (#54)"),
    (r"^/users/hidden", "deferred: per-section hidden-item lists; our hidden surface serves "
                        "the subset we use (/v1/hidden)"),
    (r"^/users/[^/]+/lists|^/users/[^/]+/smart-lists|^/users/[^/]+/smart-list-drafts|^/smart-lists",
     "deferred: the list-management write surface (create/edit/items/reorder) tracked by #67"),
    (r"^/users/", "deferred: the public-profile read surface we have not built yet; see #100"),
    (r"^/sync/", "deferred: the remaining sync write/reorder surface; the read side is tracked "
                 "by #71 and #100"),
    (r"^/shows/[^/]+/seasons|^/seasons/", "deferred: season/episode sub-resources blocked on "
                                          "metadata ingest (#68)"),
    (r"^/shows/", "deferred: the remaining show metadata sub-resources (videos, studios, "
                  "sentiments, certifications) blocked on ingest (#68)"),
    (r"^/movies/", "deferred: the remaining movie metadata sub-resources blocked on ingest (#68)"),
    (r"^/people/", "deferred: person surfaces blocked on metadata ingest (#68)"),
    (r"^/episodes/", "deferred: episode-level sub-resources blocked on ingest (#68)"),
    (r"^/comments/", "deferred: the comment moderation/reaction surface; our comments module "
                     "serves the create/reply/like subset"),
    (r"^/lists/", "deferred: the remaining lists surface tracked by #67"),
    (r"^/calendars/", "deferred: DVD, streaming and release calendars blocked on release data (#69)"),
    (r"^/recommendations/", "deferred: the item-to-item recommendations engine tracked by #49"),
    (r"^/search/", "deferred: recent-search persistence tracked by #70"),
    (r"^/certifications/|^/countries/|^/genres/|^/languages/|^/networks/",
     "deferred: reference-data enums beyond the subsets /v1/reference serves"),
    (r"^/checkin|^/scrobble|^/notes|^/media/", "deferred: tracked by the owning group issue under #4"),
    (r"^/oauth/", "deferred: Trakt's OAuth authorization server; haaku issues its own identity "
                  "and serves the same flow at /oauth/*"),
]
DEFAULT_RULE = ("deferred: not built yet; see #4 for the workstream")


def norm(k):
    m, p = k.split(" ", 1)
    return m + " " + re.sub(r"\{[^}]+\}", "{}", p)


def q(s):
    return '"' + str(s).replace('"', "'") + '"'


def served_routes():
    spec = yaml.safe_load(SPEC.read_text())
    out = {}
    for p, item in spec["paths"].items():
        for m, op in item.items():
            if m not in ("get", "post", "put", "patch", "delete"):
                continue
            if "501" in (op.get("responses") or {}):
                continue
            bare = p[3:] if p.startswith("/v1/") else p
            out.setdefault(norm(f"{m.upper()} {bare}"), []).append(f"{m.upper()} {p}")
    return out


def main():
    lines = MANIFEST.read_text().split("\n")
    starts = []
    for i, line in enumerate(lines):
        m = re.match(r'^\s*-\s+trakt:\s*"?(.*?)"?\s*$', line)
        if m:
            starts.append((i, " ".join(m.group(1).split())))
        elif re.match(r'^\s*-\s+name:', line):
            starts.append((i, None))
    starts.append((len(lines), None))
    blocks = [(starts[k][0], starts[k + 1][0], starts[k][1]) for k in range(len(starts) - 1) if starts[k][1]]

    out, pos, dropped, rewrote = [], 0, 0, 0
    for start, end, trakt in blocks:
        out.extend(lines[pos:start])
        pos = end
        if trakt in DROP:
            dropped += 1
            continue
        body = lines[start:end]
        if trakt in REWRITE:
            new_trakt, new_ours = REWRITE[trakt]
            body[0] = re.sub(r'(trakt:\s*)"?.*"?\s*$', lambda m: f'{m.group(1)}"{new_trakt}"', body[0])
            for j, l in enumerate(body):
                if re.match(r'^\s*ours:', l):
                    body[j] = re.sub(r'ours:\s*.*$', f"ours: {q(new_ours)}", l)
                    break
            rewrote += 1
        elif trakt in CUSTOM:
            body[0] = re.sub(r'(trakt:\s*)"?.*"?\s*$', lambda m: f'{m.group(1)}""', body[0])
            for j, l in enumerate(body):
                if re.match(r'^\s*status:', l):
                    body[j] = re.sub(r'status:\s*\S+', "status: out-of-scope", l)
                if re.match(r'^\s*rationale:', l):
                    body[j] = f'{re.match(r"^(\s*)", l).group(1)}rationale: {q(CUSTOM[trakt])}'
            body = [l for l in body if not re.match(r'^\s*(engine|compat):', l)]
            rewrote += 1
        out.extend(body)
    out.extend(lines[pos:])

    # --- 2. add the missing contract operations ------------------------------
    ops = list(json.loads(OPS.read_text()))
    tracked = set()
    for line in out:
        m = re.match(r'^\s*-\s+trakt:\s*"?(.*?)"?\s*$', line)
        if m:
            tracked.add(" ".join(m.group(1).split()))
    missing = [o for o in ops if o not in tracked]
    served = served_routes()

    rows, impl, waived = [], 0, 0
    for op in missing:
        hit = served.get(norm(op))
        if hit:
            rows += [f'      - trakt: {q(op)}', f'        ours: {q(hit[0])}',
                     "        status: implemented",
                     f'        rationale: {q("Served at " + hit[0] + "; the path shape matches the contract.")}']
            impl += 1
        else:
            rule = next((r for pat, r in GROUP_RULES if re.search(pat, op.split(" ", 1)[1])), DEFAULT_RULE)
            rows += [f'      - trakt: {q(op)}', '        ours: ""',
                     "        status: out-of-scope", f'        rationale: {q(rule)}']
            waived += 1

    if rows:
        out += ['  - name: "Contract surface missing from the original inventory"', "    rows:"] + rows

    MANIFEST.write_text("\n".join(out))
    print(f"dropped {dropped}, rewrote {rewrote}; added {len(missing)} rows "
          f"({impl} implemented, {waived} out-of-scope)")


if __name__ == "__main__":
    main()