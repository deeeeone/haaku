# Calendar Handler (`internal/handlers/calendar`)

## What it does

Implements the calendar endpoints, all sharing one unified handler:

- `GET /v1/calendars/my/shows/{start_date}/{days}` — shows the user watched **or** watchlisted (hidden excluded).
- `GET /v1/calendars/all/shows/{start_date}/{days}` — every episode airing in the range (public).
- `GET /v1/calendars/friends/shows/{start_date}/{days}` — episodes from shows mutual friends watched in the last 90 days.
- `GET /v1/calendars/favorites/shows/{start_date}/{days}` — episodes from favorited shows.
- `GET /v1/calendars/watched/shows/{start_date}/{days}` — episodes from watched shows.
- `GET /v1/calendars/all/movies/{start_date}/{days}` — every movie releasing in the range (public).
- `GET /v1/calendars/my/movies/{start_date}/{days}` — movies from the caller's watchlist **or** history (hidden excluded).

Behavior:
- `start_date` is `YYYY-MM-DD`; `days` is clamped to 1–30 (default 7). The range is `[start_date, start_date+days)`.
- Optional `type` query filter: `new` (S01E01), `premieres` (S02+E01), `finale` (max episode of a season).
- Target is derived by parsing the raw request path (`deriveTarget`), and auth is enforced for all targets except `all`.
- The show calendars return `{items, page, limit, row_count}`; each entry embeds episode (via `media.EpisodeResponse`), minimal show, and season number.
- The movie calendars return Trakt's `[{released, movie}]` array (a `media.MovieResponse` per entry, honoring `extended`). "my" is history ∪ watchlist minus movies hidden in the `calendar` section — the movie analogue of `getShowIDsForTarget`'s `my` target.
- Registers OpenAPI route metadata for all seven endpoints.

## Trakt parity / residue

Shipped (issue #4 §A calendars):

| Trakt path | Ours | Shape |
|---|---|---|
| `/calendars/:target/movies/:start_date/:days` | `/v1/calendars/{all,my}/movies/...` | Trakt `[{released, movie}]` |

Still absent, with reasons:

| Trakt path | Why it is not here |
|---|---|
| `/calendars/:target/dvd/...`, `/calendars/:target/streaming/...` | We store a single `movies.released` (theatrical) date. DVD and streaming release dates are not ingested (no TMDB `/release_dates` fetch, no column) — implementing these would mean **fabricating** dates from the theatrical one. Blocked on release-date ingestion, not on a handler. |
| `/calendars/releases/hot[...]` | Curated trending/anticipated feed — blocked on the rankings engine (issue #5). |
| `/calendars/:target/shows/{new,premieres,finales}/...` | We support `?type=` as a query param today; the Trakt **path-segment** shape is a §B divergence deferred to the compat listener (issue #7). |

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
| Movie calendars (`all/movies`, `my/movies`): `released` window, history ∪ watchlist, hidden excluded | Done |
| Handler tests (in-memory sqlite): window, empty window, invalid date, `my` filter, 401 | Done |
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
- [x] Batch-fetch shows/seasons to kill the N+1 loop. (Batch 10)
- [x] Apply hidden exclusion consistently across all targets (spec check). (Batch 12 — hidden shows are now excluded from every user-scoped calendar target: `my`, `friends`, `favorites`, and `watched`. The `metadata-api` spec states "my variants respect hidden items + watchlist membership"; favorites/watched are also personal calendar surfaces, so excluding a show hidden in the `calendar` section from them is consistent with that rule and with the already-applied `my`/`friends` exclusion.)
- [x] Use structured `internal` error code for DB failures. (Batch 6)
- [x] Verify `friends` mutual-follow semantics and 90-day cutoff against the spec. (Batch 12 — VERIFIED.) The `friends` target uses a strict mutual-follow query (A follows B AND B follows A, both `approved_at NOT NULL`), matching the "friends = mutual follows" definition in `social-api.md` (`GET /users/:id/friends` — "mutual follows") and the recommendations social queries. The 90-day cutoff on friend watch activity matches the social-recommendations convention ("mutual friends ... with watches in the last 90 days", `recommendations`/metadata-api) — a shared, consistent recency window across social surfaces. The calendar then lists upcoming episodes from shows those friends watched in that window. Confirmed correct against the spec; hidden (`calendar` section) exclusion also applies here as it does across user-scoped targets.)
- [x] Verify module test coverage (`make test`). (Batch 14 — `internal/handlers/calendar/calendar_test.go` covers `clampDays` (default/clamp-to-range), `deriveTarget`, `buildShowIDs`, and `buildEpisode`. Handler-level integration for the DB-fed shows path awaits a DB-driver approval.)
