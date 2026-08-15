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
| Metadata-JSONB caching with 7d TTL | Broken (never persisted) |
| Response conversion (flatrate/rent/buy) | Done |

## Oversights

- **CRITICAL: the cache is never persisted** — `serveWatchProviders` calls `storeCachedProviders(metadata, cached)`, which only mutates the **in-memory** `database.JSONMap` (watch.go:276-286). There is no `db.Model(&movie).Update("metadata", ...)` write anywhere. The `metadata` map is loaded fresh from the DB per request, so the "cached" data is discarded immediately — **every request refetches TMDB**. The whole caching layer is inert.
- **Race-prone lazy TMDB client (RESOLVED)** — `getTMDBClient` (watch.go:66-78) now guarded by `sync.Once` (Batch 2).
- **Only 3 of TMDB's provider categories mapped** — `flatrate`/`rent`/`buy` are mapped but `ads`, `free`, and `cinema` are dropped (TMDB returns those for some titles/countries).
- **Country-miss still does a full TMDB fetch per request** — even with a working cache, a requested country absent from cached results triggers a refetch of the whole provider set every time for that country (no negative caching).
- **`cpLink` hardcodes `https://www.themoviedb.org/...`** — returns a TMDB link, not a trakt-killer link; for a country with no providers this just links off-site. Also `cpLink(resp.ID, "movie", country)` passes a hardcoded `"movie"` even for shows at watch.go:235 (the show branch is reachable and would emit the wrong `/movie/` link when `country` is missing from results).
- **`itoa` hand-rolled (RESOLVED)** — replaced with `strconv.Itoa` (Batch 4).
- **`storeCachedProviders` swallows marshal errors (RESOLVED)** — now returns the error and the caller logs it (best-effort, same as `persist()`; Batch 4).
- **No Redis layer** — the design choice (metadata column) differs from the Redis-keyed caching used elsewhere; combined with the persistence bug, effectively no caching at all.
- **Local error helpers** (`notFound`/`badRequest`) duplicated from `internal/httpapi`.
- **Provider order from TMDB preserved, no sorting** — acceptable, but TMDB order can vary.

## Todos

- [ ] Persist the `watch_providers` metadata back to the DB after fetch (the cache currently does nothing).
- [x] Add `sync.Once` to the TMDB client init.
- [ ] Map `ads`/`free`/`cinema` provider categories or document their exclusion.
- [ ] Fix the `cpLink` hardcoded `"movie"` in the show country-miss path.
- [ ] Add negative caching for missing countries to avoid repeated full refetches.
- [x] `itoa` replaced with `strconv.Itoa`; `storeCachedProviders` now handles marshal errors (Batch 4).
- [x] Use shared `internal/response` helpers.
- [ ] Verify module test coverage (`make test`).
