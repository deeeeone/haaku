# Progress Handler (`internal/handlers/progress`)

## What it does

Implements playback-position reads for the current user:

- `GET /v1/sync/playback/movies` — paused/resumed movie positions, joined with the movies table (title, tmdb/imdb, overview, runtime), ordered `paused_at DESC`.
- `GET /v1/sync/playback/episodes` — paused/resumed episode positions, joined across episodes → seasons → shows (episode title/number, season number, show title + ids).

Plus the show watch/collection progress reads (issue #4 §A — thin reads over
tables we already store; no stats engine required):

- `GET /v1/shows/{id}/progress/watched` — `aired` / `completed` (episodes with a
  `user_history` row), `last_watched_at`, a per-season breakdown, and
  `next_episode`. Auth required.
- `GET /v1/shows/{id}/progress/collection` — same shape over `user_collection`
  (`completed` = collected **and** aired), `last_collected_at`. Auth required.
- `POST /v1/shows/{id}/progress/watched/reset` — deletes the caller's episode
  watch-history rows for that show and returns the recomputed watched progress.
  Auth required.

All return plain JSON arrays/objects; auth-required routes run the shared
`auth.AuthRequired()` middleware. OpenAPI metadata is registered for every route.

### How the numbers are computed

Real counts only — no fabricated fields:

- `aired` = an episode whose `aired_at` is today (UTC) or earlier. An episode
  with a NULL `aired_at` is **not** counted as aired (the schema cannot prove it
  aired), and it is not counted as upcoming either.
- `completed` = an aired episode with a `user_history` row (watched) or a
  `user_collection` row (collection). Markers are matched per episode id; if the
  dedupe index left several rows for one episode, the newest wins.
- `last_watched_at` / `last_collected_at` = the newest marker across the show's
  episodes.
- `next_episode` = the earliest episode with `aired_at` strictly after today,
  rendered with the shared `media.EpisodeResponse` builder.
- Per-season breakdown groups the aired episodes by `seasons.season_number`. A
  season row with no episodes contributes nothing.
- **Deliberately omitted:** Trakt's `reset_at` — nothing stores when a reset
  happened, so the field is left out rather than invented. Show-level
  plays/watchers/rank would need the stats engine (issue #5) and are not in this
  payload.

### What the reset deletes (and what it must not)

```
DELETE FROM user_history
 WHERE user_id = <caller>
   AND media_type = 'episode'
   AND media_id IN (SELECT id FROM episodes WHERE show_id = <show>)
```

It touches **only** that. It does **not** delete the caller's ratings,
collection entries, playback/resume positions, comments, or history for any
other show/movie, and it leaves the show-level history row (`media_type='show'`)
alone — this is an episode-progress reset, not "forget the show". Only `POST` is
offered: issue #4 lists `GET|POST .../reset`, but a GET that deletes is unsafe
and uncacheable, so the destructive verb is the only one registered.

## Completion

| Piece | Status |
|---|---|
| OpenAPI registration (2 playback + 3 show-progress routes) | Done |
| Movie playback list (joined) | Done |
| Episode playback list (triple join) | Done |
| Show watched progress (aired/completed/per-season/next) | Done |
| Show collection progress | Done |
| Show progress reset (episode-scoped) | Done |
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
- [x] Add show watch/collection progress + reset (issue #4 §A). (`show_progress.go` + `show_progress_test.go`: sqlite-backed handler tests cover zero episodes, a partially-watched show, a fully-watched show, unaired/undated episodes excluded, collection progress, the reset's exact scoping (ratings/collection/other-show/other-user/show-level rows survive), the outbox `history.removed` event, and 404 on an unknown show. The reset's outbox call goes through the `publishHistoryRemoved` var because the real insert is PostgreSQL-only `?::jsonb` SQL that sqlite cannot run — production still uses `outbox.PublishEvent`.)
