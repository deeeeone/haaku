# Reference Handler (`internal/handlers/reference`)

## What it does

Public (no-auth) read-only reference data endpoints, most proxying TMDB with a 24h Redis cache:

- `GET /v1/genres/:type` — genres for `movies`/`shows`, slugified (`slugify`: lowercase, space→`-`, `&`→`and`).
- `GET /v1/languages/:type` — TMDB languages (English name + ISO code).
- `GET /v1/countries/:type` — TMDB countries.
- `GET /v1/networks` — `SELECT DISTINCT network FROM shows` (DB, not TMDB).
- `GET /v1/certifications/:type` — TMDB certifications grouped by country.

Cache keys `reference:{category}:{type}`, TTL 24h. TMDB client lazily initialized via `getTMDBClient()`. Registered with OpenAPI.

## Completion

| Piece | Status |
|---|---|
| OpenAPI registration (5 routes) | Done |
| Genres (movies/shows) + slugify | Done |
| Languages / Countries | Done |
| Networks (DB distinct) | Done |
| Certifications grouped by country | Done |
| Redis cache (24h, graceful degradation) | Done |

## Oversights

- **`getTMDBClient` lazy init has no `sync.Once` (RESOLVED)** — now guarded by `sync.Once` (Batch 2).
- **Local error helpers duplicate `internal/httpapi`** — `writeError`/`badRequest` comment even admits "mirror httpapi exported variants". Should import the shared package.
- **Cache failure semantics inconsistent** — `getCached` falls through to upstream on Redis error (good), but a marshaled `[]` (empty upstream) is cached as a real value; an empty TMDB response would pin empty results for 24h.
- **No cache on networks DB query** — it's cached, but the query runs `DISTINCT network` full scan each cold miss; no partial-network index.
- **`slugify` is naive** — non-ASCII names (e.g. French/Japanese genres) produce slugs with characters not typical of TMDB slugs; no `unicode`, no existing-slug mapping. Genre slug drift vs TMDB `slug` field.
- **`handleCertifications` doesn't sort groups** — map iteration order is random per request; `groups` order is nondeterministic (cache serializes whichever order first hit, so it becomes stable after first fill — but non-deterministic which one).
- **Type validation duplicated** 4× (same `strings.ToLower` + allowlist in every handler) — could be one middleware/helper.
- **`/v1/networks` uses `db.Raw` with `network != ''`** — null networks silently excluded; also `NetworkItem` has only name (fine per spec, but the DB `network` field may be a JSON list of networks, not a single value — verify schema).
- **No `db == nil` check in genre/language/country/cert handlers** — they don't need DB, so fine; only `handleNetworks` guards it.

## Todos

- [x] Add `sync.Once` (or init at startup) to `getTMDBClient`.
- [x] Use shared `internal/response` error helpers.
- [x] Sort certification country groups deterministically. (Batch 12 — `handleCertifications` now `sort.Slice`s country groups by ISO code before responding/caching, so the previously map-iteration-random order is deterministic and the 24h cache serializes a stable list.)
- [x] Decide slug strategy (unicode-aware / upstream TMDB slug) — confirm genre slugs against spec. (Batch 12 — DECISION: genre slugs are ASCII-hyphenated forms of the TMDB English name, derived by `slugify`; there is no upstream TMDB genre slug to adopt, and the spec does not define one.) Findings: (1) TMDB genre objects carry only `{id, name}` — no slug field — and `data-model.md` lists the `genres` reference table as name-keyed, so there is nothing "upstream" to copy and the current `slugify` (lowercase, space→`-`, `&`→`and`) is a client-facing identity token, adequate because TMDB's canonical genre names are ASCII English. (2) IMPORTANT semantics note: `movies.genres`/`shows.genres` store the full English genre **names** (e.g. `"Science Fiction"`), and every genre filter in the codebase (`recommendations` optional `genre`, smart-list DSL) matches against those stored names via the jsonb `?` containment operator — the `/genres` slug is a display/URL token, NOT the value used for filtering. A client must filter by a stored genre name, not the slug. Unicode-aware slugging is unnecessary for the standard ASCII genre set and is not adopted; any future non-ASCII or curated slug↔name mapping is recorded as a separate gap rather than guessed at here.)
- [x] Verify `shows.network` column type (scalar vs JSON list) against the schema; fix `DISTINCT network` query accordingly. (Batch 10 — confirmed scalar `TEXT`, so `DISTINCT network` is correct; added `ORDER BY network` for a deterministic list.)
- [ ] Consider avoiding caching empty upstream results.
- [x] Verify module test coverage (`make test`). (Batch 14 — `internal/handlers/reference/reference_test.go` covers `slugify` and `cacheKey`. Handler-level integration for the TMDB-fed genre/language/country/network/cert endpoints awaits a DB-driver approval.)
