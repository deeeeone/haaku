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
- **`CreatedAt.Format("...Z")`** — comment timestamps formatted without sub-second precision; inconsistent with RFC3339 full precision elsewhere.

## Todos

- [ ] Reuse the media package episode/season builders (or the `comments` package's response type) instead of local duplicates.
- [ ] Populate external IDs (tmdb/imdb) in episode/season `IDs`.
- [ ] Fix `total_count` on the lists endpoint to reflect only visible lists (or filter in SQL).
- [ ] Consolidate error + pagination helpers into shared packages.
- [ ] Consider caching stats/ratings.
- [ ] Verify module test coverage (`make test`).
