# Progress Handler (`internal/handlers/progress`)

## What it does

Implements playback-position reads for the current user:

- `GET /v1/sync/playback/movies` — paused/resumed movie positions, joined with the movies table (title, tmdb/imdb, overview, runtime), ordered `paused_at DESC`.
- `GET /v1/sync/playback/episodes` — paused/resumed episode positions, joined across episodes → seasons → shows (episode title/number, season number, show title + ids).

Both return plain JSON arrays with `progress` + `paused_at` and enriched media info. Auth-required via hand-rolled `requireAuth` (plus a redundant inline check in each handler). Registers OpenAPI metadata for both routes.

## Completion

| Piece | Status |
|---|---|
| OpenAPI registration (2 routes) | Done |
| Movie playback list (joined) | Done |
| Episode playback list (triple join) | Done |
| Auth guard | Done |

## Oversights

- **No pagination** — unbounded result sets; a power user with thousands of paused items gets the full list (charter forbids caps, so pagination is required for scale).
- **`requireAuth` + per-handler auth check duplicated** — the middleware checks, then each handler re-asserts `user_id` and re-401s. Redundant.
- **Hand-rolled `requireAuth`/error helpers** — should be `auth.AuthRequired` + `internal/httpapi`.
- **Hand-formatted timestamps** — `"2006-01-02T15:04:05.000Z"` literals.
- **Response shapes hand-built** — movie response duplicates `media.MovieResponse` construction manually (and omits images/genres/rating) instead of using `BuildMovieResponse`; episode/show info uses local structs instead of media builders. Drift risk.
- **No `has_paused` / active-filter semantics** — returns everything in `user_playback`; no distinction between "paused" vs "resumed/expired" state, no filtering or expiry handling.
- **No `extended` handling** — query param ignored.
- **Ordering by `paused_at DESC` only** — no secondary sort key; ties are unstable.
- **Read-only module** — no progress *write* here; writes are presumably in `scrobble` (verify coverage there).

## Todos

- [ ] Add pagination (page/limit) to both endpoints.
- [ ] Remove the redundant per-handler auth check; use `auth.AuthRequired`.
- [ ] Reuse `media.BuildMovieResponse` / media builders for response enrichment.
- [ ] Clarify paused-vs-expired semantics and filter accordingly.
- [ ] Replace local helpers with shared packages.
- [ ] Verify module test coverage (`make test`).
