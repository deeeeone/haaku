# Seasons Handler (`internal/handlers/seasons`)

## What it does

Public season read endpoints:

- `GET /v1/seasons/:id` — season detail with embedded show (`media.BuildShowResponse`, honors `extended`) + public stats.
- `GET /v1/seasons/:id/stats` — watchers (distinct users on episodes in the season), plays, episodes count, avg rating.
- `GET /v1/seasons/:id/ratings` — rating distribution (avg, votes, per-star count) via `media.RatingSummary`.

Reuses media builders (`SeasonResponse`, `ShowResponse`, `RatingSummary`). No auth. OpenAPI registered.

## Completion

| Piece | Status |
|---|---|
| OpenAPI registration (3 routes) | Done |
| Season detail + embedded show + stats | Done |
| Stats endpoint | Done |
| Ratings distribution endpoint | Done |
| Reuses `media` builders | Done |

## Oversights

- **No cache** — no Redis caching of detail/stats/ratings even though `media`/`lists`/`reference` cache aggressively. Public endpoints will hammer the DB per request; stats run 4 separate queries per call.
- **Stats error handling swallowed** — `querySeasonStats` ignores all `.Error` returns; a DB error silently yields zeroed stats. Same for `queryRatingDistribution`.
- **Embedded show lookup errors ignored** — `db.First(&show...)` failure leaves `resp.Show = nil` silently (acceptable degradation but no logging).
- **Rating distribution only for `season` media_type** — season ratings query by `media_id` of type `season`; if ratings are stored per-episode elsewhere there's a mismatch. Verify data model.
- **`handleGetSeason` fetches season first, then show, then 4 stat queries** — 6+ queries for one detail response; N+1 / chatty.
- **Local error helpers** (`writeError`/`notFound`/`badRequest`) duplicated from `internal/httpapi`.
- **`parseID` returns the error from `badRequest` as a raw error** — handlers return it directly (works because it's already a `*fiber.Error`-ish response, but the flow is unusual).
- **`buildFullSeasonResponse` duplicates `media.SeasonResponse` construction** — should use a shared `media.BuildSeasonResponse` if one exists; drift risk vs `media` module.
- **Rating is computed as AVG over all ratings including 0/out-of-range** — no validation of stored rating values.

## Todos

- [ ] Add Redis caching (TTL) for season detail/stats/ratings.
- [ ] Handle errors in `querySeasonStats`/`queryRatingDistribution` (return error, 500 on failure).
- [ ] Reuse shared `media` season builder instead of the local `buildFullSeasonResponse`.
- [ ] Consolidate the detail query (single joined query or batch).
- [ ] Use shared `internal/httpapi` helpers.
- [ ] Verify season rating storage semantics against the data model.
- [ ] Verify module test coverage (`make test`).
