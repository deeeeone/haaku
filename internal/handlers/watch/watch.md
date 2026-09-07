# Watch Handler (`internal/handlers/watch`)

## What it does

Watch-provider lookup, no auth:

- `GET /v1/movies/:id/watch?country=us` — resolve movie → TMDB watch providers.
- `GET /v1/shows/:id/watch?country=us` — same for shows.

Both use `media.ResolveID` (numeric/slug), load the row from DB, then fetch providers from TMDB via a lazily-initialized client. Providers are cached **inside the media row's `metadata` JSONB column** under `watch_providers` with a `_synced_at` stamp; entries older than 7 days are refetched. Only the requested country is returned (`flatrate`/`rent`/`buy` + TMDB watch link). OpenAPI registered.

## Completion

| Piece | Status |
|---|---|
| OpenAPI registration (2 routes) | Done |
| Resolve + DB lookup + TMDB fetch | Done |
| Country validation (default `us`) | Done |
| Metadata-JSONB caching with 7d TTL | Done (persists to DB row; Batch 9 regression test) |
| Negative caching for absent countries (1h TTL) | Done (Batch 9) |
| Response conversion (flatrate/rent/buy) | Done |

## Oversights

- **Persistence WAS broken, now fixed (RESOLVED)** — `storeCachedProviders` only mutates the in-memory `metadata` JSONMap; the handlers now pass a `persist` closure (`db.Model(&row).Update("metadata", row.Metadata)`) that writes the mutated map back to the row after a TMDB fetch, so the cache survives across requests/restarts. Locked in by `TestWatchProvidersCachePersistedAcrossRequests` (Batch 9).
- **Race-prone lazy TMDB client (RESOLVED)** — `getTMDBClient` (watch.go:66-78) now guarded by `sync.Once` (Batch 2).
- **Only 3 of TMDB's provider categories mapped** — `flatrate`/`rent`/`buy` are mapped but `ads`, `free`, and `cinema` are dropped (TMDB returns those for some titles/countries).
- **Country-miss now negative-cached (RESOLVED)** — a requested country absent from cached results gets a short-TTL (1h) `watch_providers_absence` marker persisted to the metadata JSONB, so repeated requests for that country don't each trigger a full TMDB refetch (Batch 9).
- **`cpLink` hardcodes `https://www.themoviedb.org/...`** — returns a TMDB link, not a trakt-killer link; for a country with no providers this just links off-site. Also `cpLink(resp.ID, "movie", country)` passes a hardcoded `"movie"` even for shows at watch.go:235 (the show branch is reachable and would emit the wrong `/movie/` link when `country` is missing from results).
- **`itoa` hand-rolled (RESOLVED)** — replaced with `strconv.Itoa` (Batch 4).
- **`storeCachedProviders` swallows marshal errors (RESOLVED)** — now returns the error and the caller logs it (best-effort, same as `persist()`; Batch 4).
- **No Redis layer** — the design choice (metadata column) differs from the Redis-keyed caching used elsewhere; now actually effective because the metadata column is persisted to the row.
- **Local error helpers** (`notFound`/`badRequest`) duplicated from `internal/httpapi`.
- **Provider order from TMDB preserved, no sorting** — acceptable, but TMDB order can vary.

## Todos

- [x] Persist the `watch_providers` metadata back to the DB after fetch. (Batch 9 — handlers pass a `persist` closure writing the mutated metadata map to the row; regression-tested in `TestWatchProvidersCachePersistedAcrossRequests`.)
- [x] Add `sync.Once` to the TMDB client init.
- [ ] Map `ads`/`free`/`cinema` provider categories or document their exclusion.
- [ ] Fix the `cpLink` hardcoded `"movie"` in the show country-miss path.
- [x] Add negative caching for missing countries to avoid repeated full refetches. (Batch 9 — `watch_providers_absence` marker with 1h TTL in the metadata JSONB.)
- [x] `itoa` replaced with `strconv.Itoa`; `storeCachedProviders` now handles marshal errors (Batch 4).
- [x] Use shared `internal/response` helpers.
- [ ] Verify module test coverage (`make test`).
