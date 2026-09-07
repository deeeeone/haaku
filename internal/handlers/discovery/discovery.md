# Discovery Handler (`internal/handlers/discovery`)

## What it does

Implements public discovery listings:

- `GET /v1/movies/trending`, `/v1/movies/popular` — paginated movie lists ordered by `popularity DESC`, honoring `extended` (via `media.GetExtended`).
- `GET /v1/shows/trending`, `/v1/shows/popular` — same for shows.
- `GET /v1/movies/recommendations/{id}`, `GET /v1/shows/recommendations/{id}` — **stubs** that return empty arrays (Phase 2+).

Registers OpenAPI metadata for all six endpoints. Reuses `media.BuildMovieResponse`/`BuildShowResponse`. All routes are public (Auth: none).

## Completion

| Piece | Status |
|---|---|
| OpenAPI registration (6 routes) | Done |
| Trending / popular listings (movies + shows) | Done |
| Pagination (page=1, limit=20, max 100) | Done |
| `extended` support via media package | Done |
| Recommendation endpoints | Stub — return empty arrays |

## Oversights

- **"Trending" is just popularity** — `trending` and `popular` use identical queries (`popularity DESC`); the `_ string` param in `moviesList`/`showsList` is unused. Real trending (recent watch momentum) is not implemented.
- **Recommendation stubs** — handlers ignore the `{id}` param and always return `[]`; documented as Phase 2+ but registered as complete routes in OpenAPI.
- **`fiber.NewError` for DB failures** — unstructured error message rather than the envelope `internal` code (also diverges from other handlers that use `writeError`).
- **No count/total in the response** — raw arrays, no pagination envelope or `row_count`; inconsistent with calendar/comments/activity pagination shapes.
- **No `movies/collected`/`watched` variants** — only trending/popular/recommendations exist; check the spec for missing discovery endpoints.
- **Endpoint-set audit vs roadmap T1.7 (DOCUMENTED)** — roadmap T1.7 "Discovery + rankings" expects more than this module delivers: trending (watch activity, not just popularity), popular, **anticipated**, and **played / watched / collected / favorited** variants **with periods**, plus **rankings** (with a stats-worker to precompute them). Discovery currently implements only trending/popular (both `popularity DESC`); `anticipated` lives in the `recommendations` module (`/v1/media/anticipated`) and none of the per-activity (`played`/`collected`/`favorited`) or period-parameterized/ranking endpoints exist anywhere. Per Batch 9 scope these are NOT added here (endpoint additions are out of scope) — tracked as roadmap follow-up work. See roadmap.md T1.7. (Batch 9)
- **Caching added (RESOLVED)** — trending/popular movie+show listings are now Redis-cached (5m TTL), keyed by media type/kind/page/limit/extended; best-effort, DB fallback (Batch 9).

## Todos

- [x] Decide trending-vs-popular semantics (need a real trending signal or drop the distinction). (Batch 12 — DECISION: keep both endpoints but document that `trending` is currently an alias of `popular`.) The `metadata-api` spec defines trending as "by current watcher activity" and popular as "by popularity (TMDB seed + our watch data)". A real trending signal requires per-item watcher momentum, which lives in the `trending_rankings` table rebuilt by a stats-worker (data-model §9, roadmap T1.7) — that engine is not yet implemented. Rather than drop the endpoint or invent a half-signal, `trending` and `popular` both return the same `popularity DESC` ordering for now, and this equivalence is documented so clients don't expect different results. Real trending is tracked as roadmap follow-up work (Batch 9 audit).
- [ ] Implement recommendations or mark the routes as `not-implemented` in OpenAPI.
- [x] Use `internal/response` error constructors instead of `fiber.NewError`.
- [ ] Align response envelope with the API conventions (add counts/pagination metadata if the spec requires).
- [x] Add caching for hot public listings. (Batch 9 — 5m Redis TTL, keyed by type/kind/page/limit/extended; best-effort, DB fallback.)
- [x] Cross-check the endpoint set against the roadmap (T1.7). (Batch 9 — audit documented in Oversights: roadmap expects anticipated + played/watched/collected/favorited with periods + rankings, none added here per scope.)
- [ ] Verify module test coverage (`make test`).
