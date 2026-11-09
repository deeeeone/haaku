# Scrobble Handler (`internal/handlers/scrobble`)

## What it does

The watching/scrobble state machine + playback storage. Five routes:

- `POST /v1/scrobble/start` — start a session (5h `ExpiresAt`), deletes stale sessions and prior playback, advisory-locked, outbox `scrobble.started`.
- `POST /v1/scrobble/pause` — write pause session + save `UserPlayback` (resume position).
- `POST /v1/scrobble/stop` — `<1%` → 422; `>=80%` → scrobble (24h duplicate cooldown → 409, expired-session → 410), `1–79%` → pause behavior.
- `GET /v1/sync/playback` — all saved positions, optional `type` filter, `paused_at DESC`.
- `DELETE /v1/sync/playback/:media_type/:media_id` — remove a position (204 / 404).

Best-practices module: uses shared `auth.AuthRequired`, `cache.WithCacheTransaction`, `pg_advisory_xact_lock`, version-counter invalidation (`queueScrobbleInvalidation`), and outbox events (`EventScrobbleStart/Pause/Stop`). `resolveScrobbleItem` resolves TMDB→internal IDs via `media.ResolveID` (+ season/episode lookups for episode scrobbles).

## Completion

| Piece | Status |
|---|---|
| OpenAPI registration (5 routes) | Done |
| Start (expiry, cleanup, advisory lock) | Done |
| Pause (session + UserPlayback upsert) | Done |
| Stop: <1% 422 / ≥80% scrobble / else pause | Done |
| Duplicate-scrobble cooldown (24h → 409) | Done |
| Session-expiry (5h → 410) | Done |
| Playback GET/DELETE | Done |
| Cache-tx + outbox + version counters | Done |

## Oversights

- **BUG (RESOLVED): pause sessions never set `ExpiresAt`** — `handleScrobblePause` created a `ScrobbleSession` with zero `ExpiresAt`, but `handleScrobbleStop` reads the latest start/pause session and treats `time.Now().After(ExpiresAt)` as expired, so every pause→stop returned 410. Fixed: pause now sets `ExpiresAt: time.Now().Add(scrobbleSessionTimeout)` (scrobble.go:317-327).
- **`queueScrobbleInvalidation` used `BuildMediaVersionKey(mediaType, mediaID, nil, nil)`** — for episode scrobbles `mediaID` is the episode, so only `media_version:episode:<id>::` was bumped and cached show/season responses (which embed that episode's watch state) went stale. Now episode scrobbles also bump the show's slot (`media_version:show:<showID>:s<seasonNum>:e<epNum>`) via `queueEpisodeShowSlotInvalidation`, mirroring `sync.queueEpisodeInvalidations`.
- **Start returns hardcoded `"id": 0`** — session ID not exposed even though the stop response does return `session.ID`. Inconsistent.
- **`handleGetPlayback` response lacks media enrichment** — unlike `progress` (`/sync/playback/movies|episodes`) which enriches with movie/show info, this aggregate endpoint returns bare `{media_type, media_id, progress, paused_at}`. Two playback endpoints with divergent shapes.
- **`fmt.Sscanf` for `media_id` (RESOLVED)** — accepted partial parses (`"12abc"` → 12); replaced with `strconv.ParseInt` via `parsePlaybackMediaID`, which also rejects non-positive ids (400).
- **`media_type` not allowlisted (RESOLVED)** in DELETE/GET — arbitrary values silently returned empty/404; now movie/show/episode are checked and anything else returns 400.
- **Advisory lock key `(hashtext(user_id), media_id)`** — 32-bit `hashtext` collisions across users can cause cross-user lock serialization; also episode vs show scrobbles of the same user use the episode id, not the show id, so a show-level and episode-level scrobble of the same user+media don't contend (may be intended).
- **Duplicate cooldown only looks at `action='scrobble'` sessions** — `start`/`stop` (pause-path) sessions bypass; combined with the pause-`ExpiresAt` bug the cooldown window semantics are fragile.
- **`getUserID` ignores the type-assert `ok`** — returns `""` on mismatch, which is caught, but only by luck of the empty check.
- **`writeAPIError` local helper (RESOLVED)** — replaced with shared `internal/response` (Batch 0); the auth wrapper delegates to shared `auth.AuthRequired`.
- **`resolveScrobbleItem` for episodes requires TMDB show id in `episode.ids.tmdb`** — treats it as a show id; correct per Trakt schema but a misuse of the `tmdb` field name for episodes (a reader expects episode id).
- **Resolve failures overwrote the resolver's envelope with the sentinel's own text** (RESOLVED, with #93) — all three handlers answered `response.WriteError(c, 400, "invalid_request", err.Error())`. `media.ResolveID` writes its own 404/400 and returns `response.ErrHandled`, so an id that was simply not in the catalogue answered `400 {"code":"invalid_request","message":"response already written"}` — internal wording, and the resolver's 404 replaced. Now `writeResolveError` passes `ErrHandled` through untouched (returns nil) and keeps the 400 for a failure that wrote nothing; `resolve_error_test.go` pins both halves.

## Todos

- [x] Fix the pause-session `ExpiresAt` bug (410 on every pause→stop) — pause now carries `ExpiresAt: now+5h`.
- [x] Verify/invalidate season+show version keys on episode scrobbles — `queueScrobbleInvalidation` now bumps the show's episode slot (`media_version:show:<showID>:s<seasonNum>:e<epNum>`) for episode scrobbles, mirroring `sync.queueEpisodeInvalidations`.
- [x] Use `strconv.ParseInt` (via `parsePlaybackMediaID`) and validate `media_type` in playback GET/DELETE — `"12abc"` no longer parses as 12; non-movie/show/episode types return 400. Unit tests added.
- [ ] Return real session ID from start (or drop `id` from the response).
- [x] Align `/sync/playback` response shape with `progress` module or document the difference. (Batch 8 — decision: documented, not aligned. `progress` exposes two enriched, type-specific endpoints (`GET /sync/playback/movies` returns `{progress, paused_at, movie}` and `/sync/playback/episodes` returns `{progress, paused_at, episode, show}`). `scrobble`'s `GET /sync/playback` is an aggregate across *all* media types with an optional `type` filter, returning bare `{media_type, media_id, progress, paused_at}` rows — that bare, type-agnostic shape is intentionally distinct (it is the raw state-dump the sync client uses for resume positions). A single endpoint cannot carry a `movie`/`show`-specific enriched object without choosing a type per row, so the aggregate stays lean and clients needing enrichment call the `progress` routes.)
- [x] Replace `writeAPIError` with shared `internal/response`.
- [ ] Add test coverage for the 1–79% and ≥80% stop paths, expiry, and cooldown (`make test`).
