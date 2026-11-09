#!/usr/bin/env python3
"""Generate 501 parity stubs for every Trakt endpoint haaku does not serve yet.

Source of truth: docs/parity/parity.yaml — rows with an empty `ours` and a
blocked-* status. Owner: the sub-issue of #4 whose "rows this closes" table names
the row, so both the code comment and the runtime message point at the tracker
for that endpoint. Rows no group issue owns are attributed to #4 itself and
listed in the run output.
"""
import collections
import glob
import json
import re
from pathlib import Path

import yaml

WT = Path(__file__).resolve().parent.parent
MANIFEST = WT / "docs/parity/parity.yaml"
PKG = WT / "internal/handlers/paritystub"
BODIES = None  # owner map is baked in below
TRACKER = 4

# The contract Trakt publishes, generated from trakt/trakt-api (see
# docs/parity/upstream/README.md). Used only to annotate each stub with the exact
# upstream operation and response shape — the stub's own answer stays a 501.
UPSTREAM = json.loads((WT / "docs/parity/schema-index.json").read_text())


def upstream_note(trakt):
    up = UPSTREAM.get(trakt)
    if up is None:
        return "Upstream: no exact path match — see docs/parity/reconciliation.md"
    r200 = (up.get("responses") or {}).get("200")
    shape = ""
    if isinstance(r200, dict):
        if "array" in r200 and isinstance(r200["array"], dict) and r200["array"].get("keys"):
            shape = "; 200: array of {" + ",".join(r200["array"]["keys"]) + "}"
        elif r200.get("keys"):
            shape = "; 200: {" + ",".join(r200["keys"]) + "}"
    ps = up.get("parameters") or []
    pathp = [p["name"] for p in ps if p.get("in") == "path"]
    query = sum(1 for p in ps if p.get("in") == "query")
    bits = []
    if pathp:
        bits.append("path: " + ",".join(pathp))
    if query:
        bits.append(f"+{query} query")
    tail = f"; {'; '.join(bits)}" if bits else ""
    return f"Upstream {up.get('operationId')}{shape}{tail}"

GROUP_NAME = {
    66: "stats & rankings engine",
    67: "lists engine",
    68: "TMDB metadata ingest",
    69: "calendars & Watch Now data",
    70: "search engine",
    71: "season/episode-level sync",
    86: "per-item reads from stored data",
    49: "item-to-item related/recommendation engine",
}

doc = yaml.safe_load(MANIFEST.read_text())

# ---- owner mapping ----------------------------------------------------------
# Baked in so the script runs from a clean checkout: each blocked row's owning
# sub-issue of #4 (49 for the related/recommendations rows, which belong to that
# issue rather than a parity group).
OWNER = {'GET /calendars/all/dvd/{start_date}/{days}': 69, 'GET /calendars/all/streaming/{start_date}/{days}': 69, 'GET /calendars/releases/hot/{start_date}/{days}': 69, 'GET /media/trending': 66, 'GET /media/popular': 66, 'GET /movies/watched/{period}': 66, 'GET /movies/played/{period}': 66, 'GET /movies/collected/{period}': 66, 'GET /movies/hot': 66, 'GET /movies/streaming/{period}': 69, 'GET /movies/boxoffice': 69, 'GET /movies/{id}/related': 49, 'GET /movies/{id}/releases/{country}': 68, 'GET /movies/{id}/videos': 68, 'GET /movies/{id}/studios': 68, 'GET /movies/{id}/lists/{type}/{sort}': 67, 'GET /movies/{id}/stats': 66, 'GET /movies/{id}/watching': 66, 'GET /shows/watched/{period}': 66, 'GET /shows/played/{period}': 66, 'GET /shows/collected/{period}': 66, 'GET /shows/hot': 66, 'GET /shows/streaming/{period}': 69, 'GET /shows/{id}/related': 49, 'GET /shows/{id}/videos': 68, 'GET /shows/{id}/studios': 68, 'GET /shows/{id}/certifications': 68, 'GET /shows/{id}/progress/watched/reset': 66, 'GET /shows/{id}/lists/{type}/{sort}': 67, 'GET /shows/{id}/stats': 66, 'GET /shows/{id}/watching': 66, 'GET /shows/{id}/seasons/{season}/people': 68, 'GET /shows/{id}/seasons/{season}/videos': 68, 'GET /shows/{id}/seasons/{s}/episodes/{e}/people': 68, 'GET /shows/{id}/seasons/{s}/episodes/{e}/videos': 68, 'GET /people/{id}/lists/{type}/{sort}': 67, 'DELETE /recommendations/movies/{id}': 49, 'DELETE /recommendations/shows/{id}': 49, 'GET /sync/collection/episodes': 71, 'GET /sync/watchlist/seasons': 71, 'GET /sync/watchlist/episodes': 71, 'GET /watchnow/sources': 69, 'GET /watchnow/sources/{countryCode}': 69, 'GET /watchnow/justwatch_links/{country}': 69, 'POST /search': 70, 'GET /search/recent': 70, 'GET /search/recent_by_id/{type}': 70}
owner = dict(OWNER)

targets, unowned = [], []
for sec in doc["sections"]:
    for r in sec["rows"] or []:
        if (r.get("ours") or "").strip() or r["status"] not in ("blocked-engine", "blocked-data"):
            continue
        reason = " ".join(str(r.get("engine") or r.get("needs") or r.get("rationale", "")).split())
        o = owner.get(r["trakt"])
        if o is None:
            unowned.append(r["trakt"])
        targets.append((r["trakt"], "/v1" + r["trakt"].split(" ", 1)[1], r["status"], reason, o, sec["name"]))

if not targets:
    # The manifest is the source of truth: no empty `ours` + blocked row means
    # either every row is stubbed already or the stub files were deleted by hand.
    # Writing now would blank the generated files, so refuse instead.
    raise SystemExit("refusing to write: no blocked row with an empty `ours` — "
                     "the manifest is already stubbed, or the stub files were removed")

print(f"rows to stub: {len(targets)}")
print("by owner:", dict(collections.Counter(t[4] for t in targets)))
print("by status:", dict(collections.Counter(t[2] for t in targets)))
if unowned:
    print(f"rows no group issue owns ({len(unowned)}) -> attributed to #{TRACKER}:")
    for u in sorted(set(unowned)):
        print("   ", u)


def fiber_path(p):
    return re.sub(r"\{([a-z0-9_]+)\}", r":\1", p)


def auth_for(trakt):
    """Declared auth, following the contract — not "none because it 501s".

    The repo already guards this at the RouteDef level: a Sync-tagged route must be
    "required" (_tests_/sync_test.go) and a media route with /progress/ in the path
    must be "required" (_tests_/media_test.go). Everything user-scoped follows.
    """
    method, path = trakt.split(" ", 1)
    if "/progress/" in path or path.startswith("/sync") or path.startswith("/search/recent"):
        return "required"
    if method == "POST" and path == "/search":
        return "required"
    if method == "DELETE" and path.startswith("/recommendations"):
        return "required"
    if path.startswith("/users/"):
        return "optional"
    return "none"


def go_str(s):
    return '"' + s.replace("\\", "\\\\").replace('"', '\\"') + '"'


def stub_name(issue):
    return f"Stub{issue}" if issue else "StubTracker"


def link(issue):
    return issue or TRACKER


by_issue = collections.defaultdict(list)
for t in targets:
    by_issue[t[4]].append(t)

PKG.mkdir(parents=True, exist_ok=True)
for issue, rows in sorted(by_issue.items(), key=lambda kv: (kv[0] is None, kv[0])):
    gname = GROUP_NAME.get(issue, "parity tracker")
    L = ["// Code generated by scripts/parity-stubs.py from docs/parity/parity.yaml — do not edit by hand;",
         "// re-run the generator after the manifest changes.", "package paritystub", "",
         'import (', '\t"github.com/gofiber/fiber/v2"', "",
         '\t"github.com/haaku-tv/haaku/internal/openapi"', ')', "",
         f"// register{stub_name(issue)} wires the Trakt endpoints owned by #{link(issue)} ({gname})",
         "// that haaku does not serve yet. Each answers an honest 501 naming the blocker and the",
         "// tracking issue, so a Trakt client gets a real answer instead of a 404.",
         f"func register{stub_name(issue)}(api fiber.Router) {{"]
    for trakt, p, status, reason, _o, _section in rows:
        L += [f"\t// {trakt} — {status}: {reason}",
              f"\t// {upstream_note(trakt)}",
              f"\t// Tracked in #{link(issue)}: https://github.com/haaku-tv/haaku/issues/{link(issue)}",
              f"\tapi.Get({go_str(fiber_path(p))}, stubHandler({link(issue)}, {go_str(trakt)}, {go_str(reason)}))"]
    L += ["}", "",
          f"// defs{stub_name(issue)} is the registry entry for each stub above. The 501 in",
          "// StatusCodes is what makes the route grade as blocked-* in the parity manifest and",
          "// appear as a 501 in the generated spec.",
          f"func defs{stub_name(issue)}() []openapi.RouteDef {{", "\treturn []openapi.RouteDef{"]
    for trakt, p, status, reason, _o, section in rows:
        L += ["\t\t{",
              f"\t\t\tMethod:  {go_str(trakt.split()[0])},",
              f"\t\t\tPath:    {go_str(p)},",
              f"\t\t\tSummary: {go_str('Parity stub — not implemented (' + gname + ')')},",
              f"\t\t\tTags:    []string{{{go_str(section.replace(' ', ''))}}},",
              f'\t\t\tAuth:    {go_str(auth_for(trakt))},',
              f"\t\t\tStatusCodes: map[int]string{{501: {go_str('Not implemented — ' + reason[:100] + ' (#' + str(link(issue)) + ')')}}},",
              "\t\t},"]
    L += ["\t}", "}", ""]
    (PKG / f"stub_{issue if issue else 'tracker'}.go").write_text("\n".join(L))
    print(f"  wrote stub_{issue if issue else 'tracker'}.go: {len(rows)} routes")

agg = ["// Package paritystub registers a 501 stub for every Trakt endpoint haaku does not",
       "// serve yet, so the surface is complete and honest: a client asking for an",
       "// unimplemented endpoint gets `501 not_implemented` naming the blocker and the issue",
       "// that tracks it, rather than a 404 that reads as \"this endpoint does not exist\".",
       "//",
       "// Nothing here serves data. A stub disappears as its owning issue lands: the real",
       "// handler is registered by the owning package and the manifest row moves out of",
       "// blocked-* in the same PR. See docs/parity/parity.yaml and issue #4.",
       "package paritystub",
       "",
       'import (',
       '\t"fmt"',
       "",
       '\t"github.com/gofiber/fiber/v2"',
       "",
       '\t"github.com/haaku-tv/haaku/internal/openapi"',
       '\t"github.com/haaku-tv/haaku/internal/response"',
       ')',
       "",
       "func init() {",
       "\tfor _, r := range routeDefs() {",
       "\t\topenapi.Register(r)",
       "\t}",
       "}",
       "",
       "// stubHandler answers 501 with the blocker and the issue that tracks the endpoint.",
       "func stubHandler(issue int, trakt, reason string) fiber.Handler {",
       "\treturn func(c *fiber.Ctx) error {",
       "\t\treturn response.NotImplemented(c, fmt.Sprintf(",
       '\t\t\t"%s is not implemented yet: %s (tracked in #%d — https://github.com/haaku-tv/haaku/issues/%d)",',
       "\t\t\ttrakt, reason, issue, issue))",
       "\t}",
       "}",
       "",
       "// RegisterRoutes wires every parity stub. It is called FIRST in httpapi.RegisterRoutes",
       "// so a static stub segment (`/v1/movies/watched/{period}`) is matched before the",
       "// `{id}` routes that would otherwise swallow it.",
       "func RegisterRoutes(api fiber.Router) {"]
for issue in sorted(by_issue, key=lambda i: (i is None, i)):
    agg.append(f"\tregister{stub_name(issue)}(api)")
agg += ["}", "", "// routeDefs is every stub's registry entry, in a stable order.",
        "func routeDefs() []openapi.RouteDef {", "\tvar all []openapi.RouteDef"]
for issue in sorted(by_issue, key=lambda i: (i is None, i)):
    agg.append(f"\tall = append(all, defs{stub_name(issue)}()...)")
agg += ["\treturn all", "}", ""]
(PKG / "paritystub.go").write_text("\n".join(agg))
print("  wrote paritystub.go")

# ---- manifest with `ours` filled -------------------------------------------
lines = MANIFEST.read_text().split("\n")
out, cur, stubbed = [], None, 0
for line in lines:
    m = re.match(r'^(\s*)-\s+trakt:\s*"?(.*?)"?\s*$', line)
    if m:
        cur = " ".join(m.group(2).split())
    m2 = re.match(r"^(\s*)ours:\s*(.*)$", line)
    if m2 and cur:
        val = m2.group(2).strip().strip('"').strip("'")
        hit = [t for t in targets if t[0] == cur]
        if hit and not val:
            out.append(f"{m2.group(1)}ours: {go_str(hit[0][0].split()[0] + chr(32) + hit[0][1])}")
            stubbed += 1
            continue
    out.append(line)
(WT / "docs/parity/parity.yaml").write_text("\n".join(out))
print(f"  manifest updated in place: {stubbed} ours columns filled")
