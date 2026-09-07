# Calendar Handler (`internal/handlers/calendar`)

## What it does

Implements the calendar endpoints, all sharing one unified handler:

- `GET /v1/calendars/my/shows/{start_date}/{days}` — shows the user watched **or** watchlisted (hidden excluded).
- `GET /v1/calendars/all/shows/{start_date}/{days}` — every episode airing in the range (public).
- `GET /v1/calendars/friends/shows/{start_date}/{days}` — episodes from shows mutual friends watched in the last 90 days.
- `GET /v1/calendars/favorites/shows/{start_date}/{days}` — episodes from favorited shows.
- `GET /v1/calendars/watched/shows/{start_date}/{days}` — episodes from watched shows.

Behavior:
- `start_date` is `YYYY-MM-DD`; `days` is clamped to 1–30 (default 7). The range is `[start_date, start_date+days)`.
- Optional `type` query filter: `new` (S01E01), `premieres` (S02+E01), `finale` (max episode of a season).
- Target is derived by parsing the raw request path (`deriveTarget`), and auth is enforced for all targets except `all`.
- Returns `{items, page, limit, row_count}`; each entry embeds episode (via `media.EpisodeResponse`), minimal show, and season number.
- Registers OpenAPI route metadata for all five endpoints.

## Completion

| Piece | Status |
|---|---|
| OpenAPI registration for all 5 calendar routes | Done |
| Fiber route wiring (incl. auth group) | Done |
| `start_date` validation | Done |
| `days` parsing + clamp (1–30, default 7) | Done |
| `type` filter: new / premieres / finale | Done |
| Target derivation from path + per-target auth | Done |
| Target resolution: my / friends / favorites / watched / all | Done |
| Hidden-items exclusion (calendar section) | Done |
| Pagination (`page`, `limit` ≤ 100) | Done |
| N+1-style per-episode show/season lookups | Done (see oversight) |

## Oversights

- **Duplicated error helpers (RESOLVED)** — `notFound`/`badRequest`/`authRequired` re-implemented the envelope; replaced by `internal/response` + `auth.AuthRequired` (Batches 0/1). The unused `notFound` was dropped.
- **`requireAuth` and handler both re-check auth (RESOLVED)** — non-`all` routes now run `auth.AuthRequired()` and the handler-level re-check was removed (Batch 1). The public `/all` route keeps its empty `userID` handling.
- **`deriveTarget` path parsing is brittle** — hand-rolled slash counting breaks if the URL prefix/mount path changes (e.g. mounting under a sub-path). Should use a route param or route-specific handlers.
- **N+1 queries** — the response loop runs `db.First` for show and season per episode. Should batch-fetch with a single `WHERE id IN` query.
- **`excludeHiddenShowIDs` only applied to `my` and `friends`** — `favorites` and `watched` do not exclude hidden shows. Inconsistency worth confirming against the spec.
- **`friends` target has no auth-guard duplication issue only because middleware exists** — relies on `approved_at` semantics; the mutual-friend SQL is fine but reimplemented inline.
- **Uses `fiber.NewError` for DB failures** — message becomes the envelope body instead of a structured `internal` code.
- **`buildEpisode` uses `e.AiredAt.String()`** — omits `media.IDs` external IDs (tmdb/imdb) for episodes; only show-level IDs are populated.

## Todos

- [x] Replace local error helpers with `internal/response` constructors; unused `notFound` dropped.
- [x] Remove the double auth check — non-`all` routes now run `auth.AuthRequired()`; the handler no longer re-checks.
- [ ] Replace `deriveTarget` with a route param or per-target handlers.
- [ ] Batch-fetch shows/seasons to kill the N+1 loop.
- [ ] Apply hidden exclusion consistently across all targets (spec check).
- [x] Use structured `internal` error code for DB failures. (Batch 6)
- [ ] Verify `friends` mutual-follow semantics and 90-day cutoff against the spec.
- [ ] Confirm module test coverage (`make test`).
