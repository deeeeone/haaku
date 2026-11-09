#!/usr/bin/env python3
"""Reshape docs/parity/parity.yaml from instance rows onto the contract's templates.

Issue #100. Every change is an explicit entry in ROWS below: the row's new
`trakt` value, the grade it gets now that it names a template, and — when the
grade is `divergent` — the compat note saying what we serve instead. Rows are
edited line-by-line so the file's hand formatting and ordering survive.

Merges (four instance rows collapsing into one template) are expressed as
`drop`: the surviving row is the one listed with a `trakt` rewrite.
"""
import re
import sys
from pathlib import Path

MANIFEST = Path(__file__).resolve().parent.parent / "docs/parity/parity.yaml"

# template, new status (None = keep), compat note (None = keep), drop
ROWS = {
    # --- Calendars: {target} is my|all, and we serve both (calendar.RegisterRoutes).
    "GET /calendars/all/movies/{start_date}/{days}":
        ("GET /calendars/{target}/movies/{start_date}/{days}", None, None),
    "GET /calendars/all/dvd/{start_date}/{days}":
        ("GET /calendars/{target}/dvd/{start_date}/{days}", None, None),
    "GET /calendars/all/streaming/{start_date}/{days}":
        ("GET /calendars/{target}/streaming/{start_date}/{days}", None, None),

    # --- Shows: the contract spells the reset as DELETE, and uses {season}/{episode}.
    "GET /shows/{id}/progress/watched/reset":
        ("DELETE /shows/{id}/progress/watched/reset", None, None),
    "GET /shows/{id}/seasons/{season}/translations/{lang}":
        ("GET /shows/{id}/seasons/{season}/translations", "divergent",
         "we require {lang} as a path segment; the contract takes the language as a query param"),
    "GET /shows/{id}/seasons/{season}/comments":
        ("GET /shows/{id}/seasons/{season}/comments/{sort}", "divergent",
         "{sort} is accepted and ignored: one ordering, newest first"),
    "GET /shows/{id}/seasons/{season}/lists":
        ("GET /shows/{id}/seasons/{season}/lists/{type}/{sort}", None, None),
    "GET /shows/{id}/seasons/{s}/episodes/{e}/comments":
        ("GET /shows/{id}/seasons/{season}/episodes/{episode}/comments/{sort}", None, None),
    "GET /shows/{id}/seasons/{s}/episodes/{e}/lists":
        ("GET /shows/{id}/seasons/{season}/episodes/{episode}/lists/{type}/{sort}", None, None),
    "GET /shows/{id}/seasons/{s}/episodes/{e}/people":
        ("GET /shows/{id}/seasons/{season}/episodes/{episode}/people", None, None),
    "GET /shows/{id}/seasons/{s}/episodes/{e}/videos":
        ("GET /shows/{id}/seasons/{season}/episodes/{episode}/videos", None, None),

    # --- Sync families: four instance rows become the contract's one template.
    "GET /sync/watchlist/movies":
        ("GET /sync/watchlist/{type}/{sort_by}/{sort_how}", "divergent",
         "served flat at /v1/sync/watchlist/{type} for movies and shows; the {sort_by}/{sort_how} "
         "segments are not implemented (no sort), and seasons/episodes are blocked on data (#71)"),
    "GET /sync/watchlist/shows": (None, None, None, True),
    "GET /sync/watchlist/seasons": (None, None, None, True),
    "GET /sync/watchlist/episodes": (None, None, None, True),

    "GET /sync/ratings/movies":
        ("GET /sync/ratings/{type}/{rating}", "divergent",
         "served flat at /v1/sync/ratings/{type} for all four types; the {rating} filter segment "
         "is not implemented"),
    "GET /sync/ratings/shows": (None, None, None, True),
    "GET /sync/ratings/seasons": (None, None, None, True),
    "GET /sync/ratings/episodes": (None, None, None, True),

    "GET /sync/favorites/movies":
        ("GET /sync/favorites/{type}/{sort_by}/{sort_how}", "divergent",
         "served flat at /v1/sync/favorites/{type} for movies and shows; the {sort_by}/{sort_how} "
         "segments are not implemented"),
    "GET /sync/favorites/shows": (None, None, None, True),

    # --- Sync history: the contract has no per-type history list (only /{type}/{id}
    #     single-item), so these are our own surface, not parity rows.
    "GET /sync/history/movies":
        ("GET /sync/history/{type}/{id}", "divergent",
         "custom: our per-type list lives at /v1/sync/history/{type}; the contract's "
         "/sync/history/{type}/{id} is the single-item read, and the per-type lists are on the "
         "profile surface (/users/{id}/history/{type})"),
    "GET /sync/history/shows": (None, None, None, True),
    "GET /sync/history/seasons": (None, None, None, True),
    "GET /sync/history/episodes": (None, None, None, True),

    # --- Users: the contract takes {type} where we take {media_type}, and has no /me prefix.
    "GET /users/{id}/collection": (None, None, None, True),  # /collection/{type} already tracked
    "GET /users/{id}/history":
        ("GET /users/{id}/history/", None, None),
    "GET /users/{id}/history/{type}/{id}":
        ("GET /users/{id}/history/{type}/{item_id}", None, None),
    "GET /users/{id}/ratings":
        ("GET /users/{id}/ratings/", None, None),
    "GET /users/{id}/watchlist":
        ("GET /users/{id}/watchlist/{type}/{sort_by}/{sort_how}", "divergent",
         "served flat at /v1/users/{id}/watchlist/{type}; the {sort_by}/{sort_how} segments are "
         "not implemented"),
    "PUT /users/me/avatar": ("PUT /users/avatar", None, None),
    "DELETE /users/me/avatar": ("DELETE /users/avatar", None, None),
    "PUT /users/me/set_cover": ("PUT /users/set_cover", None, None),

    # --- Team: the contract path carries a trailing slash.
    "GET /team": ("GET /team/", None, None),

    # --- Search: the contract's write is /search/recent/, and recent_by_id is global-scoped.
    # POST /search -> the contract spells it POST /search/recent/, but a trailing-slash
    # path collides with our existing GET /search/recent in Fiber (the shadow guard
    # catches it), so this row keeps its own path for now and stays in the report.
    "GET /search/recent_by_id/{type}":
        ("GET /search/recent_by_id/global/{type}", None, None),
}

def norm(s):
    return re.sub(r"\{[^}]+\}", "{}", s)


def main():
    lines = MANIFEST.read_text().split("\n")

    # Row blocks: a block starts at "- trakt:" and ends just before the next row,
    # the next section header, or EOF. Boundaries are found from the ORIGINAL text
    # and the file is rebuilt in one pass, so drops and insertions cannot shift
    # each other's offsets.
    starts = []
    for i, line in enumerate(lines):
        if re.match(r'^\s*-\s+trakt:', line):
            starts.append((i, " ".join(re.match(r'^\s*-\s+trakt:\s*"?(.*?)"?\s*$', line).group(1).split())))
        elif re.match(r'^\s*-\s+name:', line):
            starts.append((i, None))
    starts.append((len(lines), None))
    blocks = [(starts[k][0], starts[k + 1][0], starts[k][1]) for k in range(len(starts) - 1) if starts[k][1]]

    seen, out, pos = {}, [], 0
    for start, end, trakt in blocks:
        out.extend(lines[pos:start])
        pos = end
        spec = ROWS.get(trakt)
        if spec:
            if len(spec) == 4 and spec[3]:
                continue                                     # merged into another row
            new_trakt, status, compat = spec
        else:
            out.extend(lines[start:end])
            continue
        if new_trakt and new_trakt in seen:
            sys.exit(f"two rows collapse onto {new_trakt!r}: {trakt!r} and {seen[new_trakt]!r}")
        if new_trakt:
            seen[new_trakt] = trakt
        body = lines[start:end]
        if new_trakt and new_trakt != trakt:
            body[0] = re.sub(r'(trakt:\s*)"?.*"?\s*$', lambda m: f'{m.group(1)}"{new_trakt}"', body[0])
        if status:
            body = [re.sub(r'status:\s*\S+', f"status: {status}", l) if re.match(r'^\s*status:', l) else l
                    for l in body]
            if status == "out-of-scope":
                body = [l for l in body if not re.match(r'^\s*(engine|compat):', l)]
            if status == "divergent":
                body = [l for l in body if not re.match(r'^\s*engine:', l)]
        if compat:
            note = compat.replace('"', "'")
            body = [l for l in body if not re.match(r'^\s*compat:', l)]
            ins = next((j for j, l in enumerate(body) if re.match(r'^\s*status:', l)), 0)
            body.insert(ins + 1, f'{re.match(r"^(\s*)", body[ins]).group(1)}compat: "{note}"')
        out.extend(body)
    out.extend(lines[pos:])
    MANIFEST.write_text("\n".join(out))
    print(f"rewrote {len(blocks)} candidate rows; manifest written")


if __name__ == "__main__":
    main()