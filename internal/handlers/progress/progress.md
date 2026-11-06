# Progress Handler (`internal/handlers/progress`)

## What it does

Implements playback-position reads for the current user:

- `GET /v1/sync/playback/movies` — paused/resumed movie positions, joined with the movies table (title, tmdb/imdb, overview, runtime), ordered `paused_at DESC`.
- `GET /v1/sync/playback/episodes` — paused/resumed episode positions, joined across episodes → seasons → shows (episode title/number, season number, show title + ids).

Both return plain JSON arrays with `progress` + `paused_at` and enriched media info. Auth-required via the shared `auth.AuthRequired()` middleware. Registers OpenAPI metadata for both routes.

## Completion

| Piece | Status |
|---|---|
| OpenAPI registration (2 routes) | Done |
| Movie playback list (joined) | Done |
| Episode playback list (triple join) | Done |
| Auth guard | Done |

## Oversights

- **No pagination** — unbounded result sets; a power user with thousands of paused items gets the full list (charter forbids caps, so pagination is required for scale).
- **`requireAuth` + per-handler auth check duplicated (RESOLVED)** — routes now run shared `auth.AuthRequired()`; per-handler checks dropped (Batch 1).
- **Hand-rolled `requireAuth`/error helpers (RESOLVED)** — `auth.AuthRequired` (Batch 1) + `internal/response` (Batch 0).
- **Hand-formatted timestamps (RESOLVED)** — now emitted via shared `response.FormatTime` (`.000Z`; Batch 3).
- **Response shapes hand-built** — movie response duplicates `media.MovieResponse` construction manually (and omits images/genres/rating) instead of using `BuildMovieResponse`; episode/show info uses local structs instead of media builders. Drift risk.
- **No `has_paused` / active-filter semantics** — returns everything in `user_playback`; no distinction between "paused" vs "resumed/expired" state, no filtering or expiry handling.
- **No `extended` handling** — query param ignored.
- **Ordering by `paused_at DESC` only** — no secondary sort key; ties are unstable.
- **Read-only module** — no progress *write* here; writes are presumably in `scrobble` (verify coverage there).

## Todos

- [x] Add pagination (page/limit) to both endpoints. (Batch 8 — §7 `X-Pagination-*` headers + `Link: rel="next"`; stable `paused_at DESC, id DESC` order.)
- [x] Remove the redundant per-handler auth check — routes run `auth.AuthRequired()` (Batch 1).
- [x] Reuse `media.BuildMovieResponse` / media builders for response enrichment (movie playback now batch-fetches and builds via `media.BuildMovieResponse`; Batch 5).
- [x] Clarify paused-vs-expired semantics and filter accordingly. (Batch 12 — DECISION: there is no "expired" state to filter; `user_playback` rows are pure resume points.) `sync-api.md` defines `GET /sync/playback` as "all saved resume points (progress %, paused_at)" with `DELETE` to clear one — there is no expiry column or expired concept on `user_playback` (unlike `scrobble_sessions`, which carry `expires_at` for the active-watching timeout). Playback rows are created on pause/stop<80% and removed when the item is completed (scrobble ≥80%), a fresh start, or explicit DELETE. So the GET returns every stored resume position — there is nothing "expired" to exclude, and adding a filter would wrongly hide positions that are still valid resume points. No code change; behavior is correct per spec.)
- [x] Replace local error helpers with `internal/response` (Batch 0); auth swap tracked at the item above.
- [x] Verify module test coverage (`make test`). (Issue #3 — DB-backed tests in `progress_test.go` exercise `handlePlaybackMovies`/`handlePlaybackEpisodes` against an in-memory sqlite DB seeded with movies / show→season→episode and `user_playback` rows, asserting the 200 path with §7 pagination headers, per-user filtering, the 500 `GetDB()` path, and the 401 auth guard via the real shared `auth.AuthRequired()`. Coverage now also asserts page/limit handling (defaults, `X-Pagination-*` values, the `Link: rel="next"` header, the 100-limit clamp, an out-of-range page, invalid-input fallback) and the 500 query-error branches — the movies batch-fetch and the episodes triple join are forced to fail by dropping the table each query needs.)
