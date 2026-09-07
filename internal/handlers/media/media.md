# Media Handler (`internal/handlers/media`)

## What it does

The **shared media-read foundation** for the API (4 files). Provides movie/show/season/episode/person detail routes and the response builders + ID resolver reused across every other handler package.

**`media.go` — media detail routes (public):**
- Movies: `GET /movies/{id}`, `/{id}/people`, `/{id}/ratings`, `/{id}/stats`, `/{id}/lists`, `/{id}/watching`.
- Shows: `GET /shows/{id}` (embeds seasons), `/{id}/seasons`, `/{id}/seasons/{season}`, `/{id}/seasons/{season}/episodes`, `/{id}/seasons/{season}/episodes/{episode}`, `/{id}/people`, `/{id}/ratings`, `/{id}/stats`, `/{id}/lists`, `/{id}/watching`.
- People: `GET /people/{id}` (with credits), `/{id}/movies`, `/{id}/shows`.

**`id_resolver.go` — canonical ID resolution (`ResolveID`)**: accepts numeric internal ids, slugs, or external ids via `?id_type=tmdb|imdb|tvdb`; disambiguates type by probing movies/shows/people tables.

**`responses.go` — response types + builders**: `IDs`, `RatingSummary`, Movie/Show/Season/Episode/Person/Credit/People response structs; `BuildMovieResponse`/`BuildShowResponse` (exported), internal builders, `GetExtended` (min/full/metadata/images/all), and `MarshalJSON` guards ensuring no nil arrays/maps.

**`translations.go` — TMDB proxy**: translation + alias endpoints (`/movies/{id}/translations/{lang}`, show/season/episode variants, `/movies|shows/{id}/aliases`) that resolve internal id → TMDB id, call TMDB v3, filter by language, and cache in Redis (24h TTL). Lazily-initialized TMDB client singleton.

## Completion

| Piece | Status |
|---|---|
| OpenAPI registration (19 + 6 routes) | Done |
| Movie/show/season/episode/person detail | Done |
| People (cast/crew) and person credits | Done |
| Ratings summary + `extended` levels | Done |
| Shared `ResolveID` (numeric/slug/external) | Done |
| Exported builders reused by other packages | Done |
| Translation/alias TMDB proxy with Redis cache | Done |
| Nil-safe `MarshalJSON` guards | Done |

## Oversights

- **Stats / lists / watching are fail-closed 501 (Batch 13)** — `handleGetMovieStats`, `handleGetMovieLists`, `handleGetMovieWatching` (and show equivalents) returned hardcoded empty/zero responses, but were registered in OpenAPI as complete 200 endpoints. They now **fail closed with 501 `not_implemented`** (real counts/activity require the stats/lists/activity engines, which aren't landed) and their OpenAPI `StatusCodes` list 501. No counts are fabricated.
- **`buildRatingSummary` returns empty distribution** — always `{}`; real per-star distributions are only computed elsewhere (episodes package). Inconsistent.
- **`buildIDs` does a per-response DB query (RESOLVED)** — each builder ran a fresh `external_ids` query; every list of N items = N extra queries (N+1 across discovery/lists/calendar too). Now batches external_ids into a single `(media_type, media_id) IN (...)` query per request via `batchExternalIDs`/`buildIDsFromLookup`, and the media package's own array builders (show detail+seasons, seasons, episodes, people) share one lookup (Batch 10). Cross-package list builders (discovery/updates/etc.) still call the single-item `Build*Response`; those batches can adopt `batchExternalIDs` in a later pass.
- **`buildIDs("season"/"episode"/"person", ...)` passes nil movie/show** — so season/episode/person `IDs` get no TMDB/IMDB populated even though `external_ids` rows exist for them (only slug/tvdb lookups run).
- **`findMediaTypeByID` fires 3 COUNT queries** — movies, then shows, then people; a failed numeric id costs 3 queries per request.
- **Local error helpers everywhere** — `notFound`/`badRequest`/`writeTranslationError` duplicated (3 copies in this package alone); other files use `fiber.NewError`.
- **`fiber.NewError` for DB failures** — unstructured messages bypass the envelope `internal` code.
- **`getTMDB` singleton is race-prone (RESOLVED)** — now guarded by `sync.Once` (Batch 2).
- **TMDB error responses leak detail (RESOLVED)** — `proxyTranslations`/alias handlers now log the raw upstream error server-side via `tmdbUpstreamError` and return a generic `internal` message.
- **TMDB proxy has no upstream fallback** — if TMDB is down, translations/aliases 500 even though data could be cached or degraded.
- **`getCachedTranslations`/`getCachedAliases` ignore lang in the key for aliases** — fine, but `proxyTranslations` keys include lang while filtering also matches ISO3166; keys and filters can disagree (same key for `?lang=en` matched by country).
- **`handleGetShow` embeds all seasons** regardless of `extended` — `ShowResponse.Seasons` is always populated (non-nil), which can be heavy.

## Todos

- [x] Implement or explicitly mark as `not-implemented` the stats/lists/watching stubs. (Batch 13 — DECISION: all six movie/show stats/lists/watching handlers now fail closed with 501 `not_implemented` until the stats/lists/activity engines land; the previous hardcoded-zero/empty responses fabricated results and were removed. Ratings summaries remain real.)
- [ ] Populate real rating distributions in `buildRatingSummary` or drop the field until implemented.
- [x] Batch external-id lookups in `buildIDs` (single query per batch) to kill N+1s. (Batch 10)
- [ ] Populate TMDB/IMDB for season/episode/person IDs from `external_ids`.
- [x] Use `sync.Once` for the TMDB client singleton.
- [x] Sanitize/log TMDB upstream errors instead of echoing raw text. (Batch 6)
- [x] Replace duplicated error helpers with `internal/response`.
- [ ] Reconsider always-embedded seasons on show detail.
- [ ] Verify module test coverage (`make test`).
