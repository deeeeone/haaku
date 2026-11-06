# Episodes Handler (`internal/handlers/episodes`)

## What it does

Implements episode detail endpoints (public, Auth: none):

- `GET /v1/episodes/{id}` — episode detail with embedded season, show (extended-aware), and stats.
- `GET /v1/episodes/{id}/stats` — watchers (distinct users), plays (history rows), average rating.
- `GET /v1/episodes/{id}/ratings` — rating distribution (per-star counts).
- `GET /v1/episodes/{id}/comments` — paginated comments on the episode.
- `GET /v1/episodes/{id}/lists` — public / link_only lists containing the episode (paginated).

All handlers verify the episode exists before serving sub-resources. Registers OpenAPI metadata for all five routes.

## Completion

| Piece | Status |
|---|---|
| OpenAPI registration (5 routes) | Done |
| Episode detail + embedded season/show/stats | Done |
| Stats endpoint (watchers/plays/rating) | Done |
| Rating distribution endpoint | Done |
| Comments endpoint (paginated) | Done |
| Lists endpoint (privacy filtered) | Done |
| 404 handling per route | Done |

## Oversights

- **`queryRatingDistribution` returns `media.RatingSummary`** but `handleGetEpisodeRatings` casts to JSON directly — response shape shared across media; fine but no per-episode wrapper.
- **N+1 / per-row fetch** — `handleGetEpisode` does 3 separate `db.First` calls (episode→season→show) plus stats queries; acceptable at this scale but not batched.
- **`buildEpisodeIDs`/`buildSeasonIDs` drop external IDs** — only internal `id` populated (no tmdb/imdb), unlike `calendar.buildShowIDs` which fills them. `buildEpisodeResponse` also re-implements the media package builder rather than reusing it.
- **List endpoint `total_count` counts all items** (incl. private lists) while `data` filters to public/link_only — counts can exceed returned rows; misleading pagination.
- **Comment response re-declared locally** — `commentResponse` in `queryMediaComments` duplicates the shape in `internal/handlers/comments` and truncates fields (no `updated_at`, `edited_at`); drifts over time.
- **Local error + pagination helpers** — `writeError`/`notFound`/`badRequest`/`parsePaginationHelper` duplicate `internal/httpapi` and other handler packages.
- **Stats computed live per request** — no caching/versioning for the hot stats path; `COALESCE(AVG(rating),0)` scans are unindexed-friendly but heavy at scale.
- **`CreatedAt.Format("...Z")` (RESOLVED)** — comment timestamps now emit `.000Z` via `response.FormatTime` (Batch 3).

## Todos

- [x] Reuse the media package episode/season builders (`media.BuildEpisodeResponse`/`BuildSeasonResponse`) instead of local duplicates (Batch 5).
- [ ] Populate external IDs (tmdb/imdb) in episode/season `IDs`.
- [x] Fix `total_count` on the lists endpoint to reflect only visible lists (or filter in SQL). (Batch 8 — count and data query now `JOIN lists` and filter `privacy IN (public, link_only)` in SQL; `total_count` matches returned rows.)
- [x] Consolidate error + pagination helpers into `internal/response`.
- [ ] Consider caching stats/ratings.
- [x] Verify module test coverage (`make test`). (Issue #3 — DB-backed handler tests in `episodes_test.go` now exercise the `get`/`stats`/`ratings`/`lists`/`comments` endpoints against an in-memory sqlite DB (via the new `database.SetDBForTest` test seam), asserting 200 paths with seeded rows, 404 error codes, both 500 paths (`database unavailable` when `GetDB()` fails; `database query error` on a real query failure), and page/limit handling on the paginated endpoints (defaults, 100-limit clamp, out-of-range page, invalid-input fallback). Note: a genuine 400 for a malformed (non-numeric) id is currently unreachable on this module — `response.ParseIDParam` writes the 400 envelope but returns a nil error, so the handler falls through to the 404 not-found branch.)
