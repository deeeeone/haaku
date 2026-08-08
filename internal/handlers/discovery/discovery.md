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
- **No cache layer** — popular/trending are hot, public, read-heavy endpoints; no Redis caching or version invalidation.

## Todos

- [ ] Decide trending-vs-popular semantics (need a real trending signal or drop the distinction).
- [ ] Implement recommendations or mark the routes as `not-implemented` in OpenAPI.
- [ ] Use `internal/httpapi` error constructors instead of `fiber.NewError`.
- [ ] Align response envelope with the API conventions (add counts/pagination metadata if the spec requires).
- [ ] Add caching for hot public listings.
- [ ] Cross-check the endpoint set against the roadmap (T1.7).
- [ ] Verify module test coverage (`make test`).
