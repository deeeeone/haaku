# Media Handler (`internal/handlers/media`)

## What it does

The **shared media-read foundation** for the API (4 files). Provides movie/show/season/episode/person detail routes and the response builders + ID resolver reused across every other handler package.

**`media.go` — media detail routes (public):**
- Movies: `GET /movies/{id}`, `/{id}/people`, `/{id}/ratings`, `/{id}/stats`, `/{id}/lists`, `/{id}/watching`, `/{id}/comments/{sort}`.
- Shows: `GET /shows/{id}` (embeds seasons), `/{id}/seasons`, `/{id}/seasons/{season}`, `/{id}/seasons/{season}/episodes`, `/{id}/seasons/{season}/episodes/{episode}`, `/{id}/people`, `/{id}/ratings`, `/{id}/stats`, `/{id}/lists`, `/{id}/watching`, `/{id}/next_episode`, `/{id}/last_episode`, `/{id}/comments/{sort}`, `/{id}/seasons/{season}/comments/{sort}`, `/{id}/seasons/{season}/episodes/{episode}/comments/{sort}`.
- People: `GET /people/{id}` (with credits), `/{id}/movies`, `/{id}/shows`.

**`id_resolver.go` — canonical ID resolution (`ResolveID`)**: accepts numeric internal ids, slugs, or external ids via `?id_type=tmdb|imdb|tvdb`; disambiguates type by probing movies/shows/people tables.

**`responses.go` — response types + builders**: `IDs`, `RatingSummary`, Movie/Show/Season/Episode/Person/Credit/People response structs; `BuildMovieResponse`/`BuildShowResponse` (exported), internal builders, `GetExtended` (min/full/metadata/images/all), and `MarshalJSON` guards ensuring no nil arrays/maps.

**`translations.go` — TMDB proxy**: translation + alias endpoints (`/movies/{id}/translations/{lang}`, show/season/episode variants, `/movies|shows/{id}/aliases`) that resolve internal id → TMDB id, call TMDB v3, filter by language, and cache in Redis (24h TTL). Lazily-initialized TMDB client singleton.

## Completion

| Piece | Status |
|---|---|
| OpenAPI registration (21 + 6 routes) | Done |
| Movie/show/season/episode/person detail | Done |
| Show `next_episode` / `last_episode` (episode-boundary reads, 204 when none) | Done |
| People (cast/crew) and person credits | Done |
| Ratings summary + `extended` levels | Done |
| Shared `ResolveID` (numeric/slug/external) | Done |
| Exported builders reused by other packages | Done |
| Translation/alias TMDB proxy with Redis cache | Done |
| Nil-safe `MarshalJSON` guards | Done |

## Trakt parity / residue (issue #4 §A)

Shipped: `GET /shows/{id}/next_episode` and `/shows/{id}/last_episode` — thin reads over `episodes.aired_at` (next = earliest episode airing today/later; last = latest aired). Both return the shared `EpisodeResponse`, and `204` when there is nothing on that side of today.

Shipped: `GET /{movies,shows}/{id}/comments/{sort}` and the nested
`GET /shows/{id}/seasons/{season}/comments/{sort}` /
`GET /shows/{id}/seasons/{season}/episodes/{episode}/comments/{sort}` — thin
reads over the `comments` table (see `internal/handlers/comments/comments.md` for
the sort table). Each resolves its parent chain with real lookups and 404s on a
missing parent; the shared `comments.ListMediaComments` does the querying.

Not here, with reasons:

| Trakt path | Why |
|---|---|
| `/movies/{id}/related`, `/shows/{id}/related` | Similarity/related engine — issue #5. |
| `/movies|shows/{id}/videos`, `/releases/{country}` | No videos / per-country release data is ingested (the schema keeps one `trailer_url` and one `released` date); returning anything else would be fabricated. |
| `/movies|shows/{id}/studios` | No studios table. |
| `/movies/{id}/boxoffice` | Box office needs revenue ranking — issue #5. |
| `/shows/{id}/certifications` | We store one `certification` string; Trakt wants a per-country map. Needs certification ingestion. |

### Season / episode sub-resources left out (issue #4 §A)

The issue also lists `seasons/{season}/{ratings,stats,people,videos,lists,translations}`
and `episodes/{episode}/{people,videos,lists}` under the show path. None are
stubbed (stubbing is forbidden); each is skipped for a stated reason:

| Sub-resource | Reason |
|---|---|
| `seasons/{s}/ratings`, `episodes/{e}/ratings` | Real per-season/episode rating distributions are computed in `internal/handlers/episodes` (`/v1/episodes/{id}/ratings`) but not exposed from the show path yet; the media package's `buildRatingSummary` returns an empty distribution, so wiring it here would ship a hollow payload. Needs the rating-distribution helper to move to a shared builder — follow-up, not engine-blocked. |
| `seasons/{s}/stats`, `episodes/{e}/stats` | Engine-blocked (issue #5): watchers/plays/collectors need the stats/rankings engine. `/v1/shows|movies/{id}/stats` already fail closed with 501 for the same reason. |
| `seasons/{s}/people` | No season-level credit rows are ingested (`credits` is keyed by movie/show only); a lookup would always be empty, i.e. fabricated. |
| `{movies,shows,seasons,episodes}/{id}/videos` | Data absent: nothing ingests videos. |
| `seasons/{s}/lists`, `episodes/{e}/lists` | Lists engine not landed; the existing movie/show `lists` routes fail closed with 501. Note `/v1/episodes/{id}/lists` exists in the flat surface, so episode lists are reachable there. |
| `seasons/{s}/translations` | Covered by the flat `/v1/seasons/{id}/translations/{lang}` TMDB proxy in `translations.go`; only the nested path is missing (a routing, not a data, gap). |

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
- [x] Verify module test coverage (`make test`). (Batch 14 — `internal/handlers/media/media_test.go` adds unit coverage for the pure builders (`buildRatingSummary`, `buildMovieResponse`/`buildShowResponse`/`buildSeasonResponse`/`buildEpisodeResponse` min vs full/all extended levels, nil-map MarshalJSON guard, `GetExtended`), the TMDB JSON parsing helpers (`filterTranslations` incl. name-fallback + empty-slice contract, `parseAliases`), and `isNumeric`. Handler-level integration coverage for DB-backed get/people/translation proxy paths awaits a DB-driver approval.)
