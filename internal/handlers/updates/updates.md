# Updates Handler (`internal/handlers/updates`)

## What it does

Public "what changed recently" endpoints (sync-scraper friendly), no auth:

- `GET /v1/movies/updates/:start_date` — movies with `updated_at >= start_date`, ordered `updated_at DESC`.
- `GET /v1/shows/updates/:start_date` — same for shows.
- `GET /v1/people/updates/:start_date` — same for people.

Each paginates (`page`/`limit`, default 25, max 100) and honors `extended` (min/full/metadata/images/all) for movie/show responses. Builds `media.MovieResponse`/`ShowResponse`/`PersonResponse` via **local** builder functions. OpenAPI registered.

## Completion

| Piece | Status |
|---|---|
| OpenAPI registration (3 routes) | Done |
| Movies / shows / people updated-since | Done |
| Pagination + extended handling | Done |
| Response builders | Done |

## Oversights

- **Response has no `updated_at`** — clients can't tell which entity changed or when; Trakt's updates endpoint returns timestamps. The list is just media objects.
- **Local builders duplicate `media`'s** — `buildMovieResponse`/`buildShowResponse`/`buildPersonResponse`/`buildMovieIDs`/`buildShowIDs` reimplement what `media` already exports (`BuildMovieResponse`, `BuildShowResponse`); the extended switch uses `fallthrough` magic and an `if extended == "all"` re-set block that sets fields twice. Drift risk (already diverging: no trailer/status handling parity).
- **`case "images":` is empty** — images are always embedded in the base response, so the "images" extended level is a no-op; also `"metadata"` only sets `Metadata` (no fallthrough to images semantics).
- **`notFound` helper is dead code** — defined, never called.
- **No caching** — unlike `media`/`reference`/`lists`, these public read endpoints hit the DB directly each call; updates feeds are exactly the kind of thing that should be Redis-cached with a short TTL.
- **Date-only `start_date`** — `time.Parse("2006-01-02", ...)`; sub-day precision is lost and Trakt allows timestamp-style start dates. Also no `end_date`/`type` filters.
- **Error helpers** — `badRequest` local; DB errors via `fiber.NewError` (which the shared error handler formats — OK, but inconsistent with sibling modules that write the envelope inline).
- **Empty results are `null`, not `[]`** — `make([]media.MovieResponse, 0, len(movies))` only guards the happy path length; a `nil` movies slice yields `null` in JSON for the no-results case.
- **No `row_count`/`total` in the envelope** — array-only response means clients can't page cleanly.

## Todos

- [ ] Include `updated_at` (and entity id) in responses.
- [x] Reuse `media` builders (`BuildMovieResponse`/`BuildShowResponse`/`BuildPersonResponse`) instead of the local duplicates; person IDs now preserve tmdb/imdb in media's builder (Batch 5).
- [ ] Add Redis caching with a short TTL.
- [ ] Consider supporting timestamp `start_date` and an `end_date` filter per spec.
- [x] Return `[]` (never `null`) for empty result sets. (Batch 8 — response built with `make([]T, 0, …)`, so an empty/no-match result serializes as `[]`; a nil underlying slice never reaches the JSON encoder.)
- [x] Add pagination metadata to the response. (Batch 8 — the array body is kept (matching the spec-compliant `webhooks`/`sync` convention); §7 `X-Pagination-*` headers + `Link: rel="next"` now carry `page`/`limit`/`total`/pages so a client can page deterministically against the stable `updated_at DESC, id DESC` order. No `row_count`/`total` envelope field is needed since headers suffice.)
- [x] Remove dead `notFound` helper.
- [ ] Verify module test coverage (`make test`).
