# Sync Handler (`internal/handlers/sync`)

## What it does

The sync workhorse — 31 endpoints over five collections plus the last-activities delta (all auth-required, per-handler checks):

- **History** (4): GET paginated (+ `type`, `start_at`, `end_at` filters), GET single item, POST add (movies + shows expanded to episodes by season, all-episode expansion), POST remove (by IDs or media objects).
- **Collection** (4): mixed GET, GET `/collection/episodes` (Trakt's collected-episode shape), POST add, POST remove (movies/shows/seasons/episodes; seasons expand to episode rows).
- **Watchlist** (4): GET (+`sort_by` rank/listed/title), POST add (assigns incrementing rank), POST remove, PUT reorder.
- **Ratings** (3): GET, POST upsert (`OnConflict` on user+media), POST remove. Episode ratings carry `season`/`number` and resolve series→season→episode, exactly as history add does; an episode reference without them is rejected (400).
- **Favorites** (3): GET, POST add (ranked), POST remove.
- **Per-type reads** (12): GET `/v1/sync/{history,collection,watchlist,ratings,favorites}/{segment}` — see "Per-type reads" below.
- **Last activities** (1): GET `/v1/sync/last_activities` — Trakt's "what changed since" delta, per-collection timestamps sourced from the rows we already write (issue #6). See "Last activities" below.

Writes use `cache.WithCacheTransaction` with version-counter invalidation (`queueUserInvalidation`/`queueMediaInvalidation`/`queueEpisodeInvalidations`) and outbox events (`EventHistoryAdded`, `EventCollectionAdded`, `EventWatchlistAdded`, `EventRatingAdded`, `EventFavoriteAdded`). Batch responses follow the Trakt `added/not_found/duplicates` shape.

## Request ids (`idsObj`)

Every write endpoint addresses its media through the same `idsObj`
(`{"tmdb":…,"tvdb":…,"imdb":…}`). Both id forms decode, and that is a contract
requirement rather than tolerance: haaku **serves** `tvdb` as a JSON string
(`media.IDs.TVDB`; `external_ids.external_id` is `TEXT`), so a client that keeps
the ids it was handed and posts them back — `/v1/media/resolve` → detail page →
watchlist add is exactly that path — must not be rejected, while a
Trakt-shaped client sends the same id as a number. `idsObj.UnmarshalJSON` accepts
either; absent, `null` and `""` all mean "not supplied" (`0`), and a non-numeric
string is still an error that names the field. The struct keeps `int` fields, so
the `not_found` echo of a dropped id is unchanged. Issue #87.

## Per-type reads (`GET /v1/sync/<kind>/<segment>`)

Trakt exposes each sync collection a second time, filtered by media type. Those
variants are **the same list handlers with the media type pinned by the path
segment** — no second query, no second response builder: `per_type_reads.go`
holds one table (`perTypeReads`) that the routes, the OpenAPI defs and the
tests all read, and a per-type registration is
`sync.Get("/"+kind+"/"+segment, forceMediaType(mediaType), handler)`. The pinned
type wins over `?type=` (`mediaTypeFilter`), so a per-type route cannot be
widened back to the mixed list; the mixed list still honours `?type=` and
`?type=` absent still means "all".

A variant exists only where the backing table can hold rows of that media type —
a route that could only ever answer `[]` would be a stub, and is not registered:

| Collection | `media_type` stored | Variants served | Variants blocked, and why |
|---|---|---|---|
| `user_history` (0006) | `movie`, `episode` | `/history/movies`, `/history/episodes` | `/history/shows`, `/history/seasons` — we store no show/season history row. Trakt's `shows`/`seasons` variants are the *episode* rows with an embedded `show`/`season` object; the history row carries `season_id`/`episode_id` but no show identity, so a `shows` variant would return a byte-identical duplicate of `/history/episodes` labelled `media_type: "episode"`. Serving it needs the embedded media object (a shape change), or a show-level history row — a separate piece of work. |
| `user_collection` (0007) | `movie`, `show`, `episode` | `/collection/movies`, `/collection/shows`; `/collection/episodes` is a dedicated Trakt-shaped read (issue #71), not a pinned row variant | `/collection/seasons` — season writes expand to their episode rows, so no season-level collection row exists. The episode rows are real: `handleAddCollection` expands seasons and shows with seasons to `media_type='episode'` rows. |
| `user_watchlist` (0008) | `movie`, `show` | `/watchlist/movies`, `/watchlist/shows` | `/watchlist/seasons`, `/watchlist/episodes` — same gap: `user_watchlist` is movie/show only and watchlist add accepts nothing else. |
| `user_ratings` (0009) | `movie`, `show`, `season`, `episode` | `/ratings/movies`, `/ratings/shows`, `/ratings/seasons`, `/ratings/episodes` | none — all four are real `media_type` values on this table. |
| `user_favorites` (0010) | `movie`, `show` | `/favorites/movies`, `/favorites/shows` | none beyond the issue's list: favorites are movie/show only, which is exactly what Trakt's favorites endpoint offers. |

**Response shape decision.** A per-type response is the mixed list's envelope
(`{"items": [...], "page", "limit", "row_count"}`) with the same row fields
(`id`, `media_type`, `media_id`, and the collection's stored timestamp —
`watched_at` / `collected_at` / `listed_at` / `rated_at` / `favorited_at`).
It is deliberately **not** Trakt's nested `movie`/`show`/`episode` object with
an `ids` block: our sync rows are not joined to their media (the mixed lists
don't join either), and our canonical ids are internal — the external
`trakt`/`tmdb`/`imdb` ids live in `external_ids`, which a read of these
collections does not currently touch. Turning one per-type route into Trakt's
media-object shape while its mixed twin stayed row-shaped would be worse than
either consistency or parity; the object shape is a follow-up across all five
collections (and belongs with the `shows`/`seasons` history variants above).

**`?extended=min` / `?minimal` — decided, not implemented.** The narrowing the
issue asks for exists to drop Trakt's extra payload (translations, images,
`rating_summary`, nested media). Our sync rows carry none of that: the row shape
above *is* the minimal shape, and there is nothing for `min`/`minimal` to
remove and nothing for `full` to add without the media join. Implementing the
parameter now would be a no-op that lies about what the server can widen to, so
it is left to the same follow-up as the media-object response shape.

Empty is `"items": []` — never `null` — for every variant, in the tests and in
the handlers (`build*Responses` always allocate).

Handler tests: `per_type_reads_test.go` (sqlite in-memory, real PostgreSQL when
`DATABASE_URL` is set) — per-variant pinned type, caller scoping, non-empty
fixtures, `[]`-not-null for an empty account, pagination, path-wins-over-`?type=`,
and a table check that no unbacked `(collection, media_type)` pair is declared.

## Completion

| Piece | Status |
|---|---|
| OpenAPI registration (31 routes) | Done |
| History GET/add/remove (show→episode expansion) | Done |
| Collection GET/add/remove | Done (issue #71 adds episode rows, season/show expansion, and `/collection/episodes`) |
| Watchlist GET/add/remove/reorder | Done |
| Ratings GET/upsert/remove | Done |
| Favorites GET/add/remove | Done |
| Per-type reads (12 GET variants, `per_type_reads.go`) | Done (issue #4) — pinned `media_type` over the same handlers; blocked variants documented above |
| Last activities (GET `/v1/sync/last_activities`) | Done (issue #6) — real per-collection aggregates, no cache |
| Cache-tx + version invalidation + outbox | Done |

## Last activities (`GET /v1/sync/last_activities`)

The delta endpoint every Trakt client polls before syncing: cache the timestamps
locally and only re-fetch a collection whose timestamp moved. The response
mirrors Trakt's `last_activities` shape — a section per collection, each an
object of Trakt's timestamp field names; a section the caller has no rows for is
`null` (never an object of nulls or a placeholder `now`). `all` is the greatest
activity across every section.

Every leaf is the newest matching row's timestamp, scoped to the caller
(`handlers/sync/last_activities.go`). It is read as `ORDER BY <ts> DESC LIMIT 1`
— the same value as `MAX(<ts>)`, but selecting the bare column keeps the
driver's native time decoding (an aggregate loses the column type, and sqlite
then returns raw text):

| Section | Field | Source (newest `col` where `user_id = caller`) |
|---|---|---|
| `movies` | `watched_at` / `collected_at` / `rated_at` / `watchlisted_at` / `favorited_at` / `commented_at` / `paused_at` / `hidden_at` | `user_history.watched_at` / `user_collection.collected_at` / `user_ratings.rated_at` / `user_watchlist.listed_at` / `user_favorites.favorited_at` / `comments.created_at` / `user_playback.paused_at` / `user_hidden.hidden_at` (all `media_type='movie'`) |
| `episodes` | `watched_at` / `collected_at` / `rated_at` / `commented_at` / `paused_at` | same tables, `media_type='episode'` (episode collection rows, issue #71) |
| `shows` | `rated_at` / `watchlisted_at` / `favorited_at` / `commented_at` / `hidden_at` | same tables, `media_type='show'` |
| `seasons` | `rated_at` / `commented_at` / `hidden_at` | same tables, `media_type='season'` |
| `ratings` | `updated_at` | `user_ratings.updated_at` |
| `watchlist` | `updated_at` | `user_watchlist.created_at` |
| `favorites` | `updated_at` | `user_favorites.created_at` |
| `collection` | `updated_at` | `user_collection.created_at` |
| `history` | `updated_at` | `user_history.created_at` |
| `comments` | `liked_at` | `comment_likes.created_at` |
| `notes` | `updated_at` | `user_notes.updated_at` |
| `lists` | `liked_at` / `updated_at` / `commented_at` | `list_likes.created_at` / `lists.updated_at` / `comments.created_at` (where `list_id IS NOT NULL`) |
| `follows` | `updated_at` | `user_follows.created_at` (`follower_id = caller`) |
| `checkins` | `updated_at` | `user_checkins.created_at` |
| `playback` | `updated_at` | `user_playback.paused_at` (no created/updated column) |

Explicit nulls (fields Trakt defines that we do **not** store — null, never
invented): `episodes.watchlisted_at`, `seasons.watchlisted_at` — our
`user_watchlist` only carries `movie`/`show` (see the watchlist gap below). Trakt's top-level
`recommendations`, `account`, and `saved_filters` sections are not part of our
response at all (we store no per-user activity time for them).

**Freshness / caching decision: per-request aggregates, no cache.** Each request
runs a bounded set of index-ordered latest-row reads over that one user's rows
(one round trip per leaf above; there is no N+1 over media). The endpoint is the
delta *signal*: a stale timestamp makes a client skip a collection that actually
changed, so it must reflect committed writes immediately. Our cache layer
invalidates version counters **after** commit (async), so a cached copy would
serve briefly-stale timestamps to exactly the clients that depend on freshness.
The server-side `*_updated_at` markers use the row's own `created_at`/
`updated_at`, so they advance on every write. If this endpoint proves hot, a
short-TTL cache (or a persisted per-user activity row updated in the same
transaction) is the natural next step — none exists today.

## Oversights

- **Sync writes rejected the `ids` block the API itself served (RESOLVED, issue #87)** — `idsObj` declared `tvdb` (and `tmdb`) as `int` while every media response serialises tvdb as a **string** (`media.IDs.TVDB`; `external_ids.external_id` is `TEXT`). A client that echoed back the ids it was served — the web UI's search → watchlist path, via `/v1/media/resolve` — got `400 invalid JSON body` on all ten write endpoints, with the message blaming the JSON syntax, which was valid. `idsObj.UnmarshalJSON` now accepts a number or a numeric string; a non-numeric string is still refused, naming the field. Regression tests: `ids_test.go` (`TestIDsObjAcceptsStringOrNumericIDs`, `TestSyncWriteBodiesAcceptTheIDsBlockTheAPIServes`, `TestAddRatingResolvesAStringTVDBID`). Related and NOT fixed here: the blanket `invalid JSON body` message for every decode failure, and the same int-only `ids.tmdb` in checkin/scrobble/list items — issue #88.

- **WRONG OUTBOX EVENT NAMES on removals/reorder (RESOLVED)** — copy-paste bugs:
  - `handleRemoveCollection` publishes `EventCollectionAdded` with `{"deleted": ...}` (sync.go:737) — **kept as-is**: the webhook catalog defines `collection.updated` as the add+remove event, so this is the correct constant (comment added).
  - `handleRemoveRatings` published `EventRatingAdded`, `handleRemoveFavorites` published `EventFavoriteAdded`, `handleReorderWatchlist` published `EventWatchlistAdded` (sync.go) — **fixed**: the webhook catalog (`haaku-plan/03-api/webhooks.md`) defines no rating/favorite removal or watchlist-reorder events, so these handlers now emit nothing.
- **Hardcoded `0` in add responses (RESOLVED)** — `handleAddWatchlist` returned `"added":{"movies":0,"episodes":0}` instead of real counts; `handleAddFavorites` returned `"movies":0,"shows":0` with `added` unused; `handleAddRatings` lumped ALL adds into `"movies"`. Now all three track real per-type `added`/`duplicates` (`addedMovies/Shows`, `dupMovies/Shows`, `addedSeasons/Episodes`) and return them.
- **No requireAuth middleware on routes (RESOLVED)** — `/sync` group now runs `auth.AuthRequired()` via `sync.Use`; per-handler `c.Locals("user_id")` checks dropped (Batch 1).
- **`handleGetHistoryItem` never validated `media_type` (RESOLVED)** — the route param was used verbatim in the WHERE while `media.ResolveID` may resolve a different type; now the resolved type must match the param or the request returns 400.
- **`parseWatchedAt` silently fell back to `now` (RESOLVED)** — invalid/unknown timestamps became "now", silently corrupting watch history on client typos. Invalid RFC3339 strings now error and the offending item is reported under `not_found` with `"error": "invalid watched_at"` (batch keeps processing the rest). `"unknown"` semantics (aired_at vs now) still need a spec decision.
- **`resolveByTMDB` bypasses `media.ResolveID`** — mutates the context (`c.Set("id_type", ...)`) and hand-rolls an `external_ids` lookup + `verifyMediaExists` per item; duplicates the resolver and does an extra query per item (N+1 on bulk adds).
- **No advisory locks on rank assignment (RESOLVED)** — `handleAddWatchlist`/`handleAddFavorites` now take a per-user advisory lock (`pg_advisory_xact_lock(hashtext('watchlist'|'favorites'), hashtext(userID))`) before reading `MAX(rank)`, so concurrent adds can't produce duplicate ranks.
- **Reorder was 0-based and partial (RESOLVED)** — ranks are now 1-based (`i+1`) and the payload must be the complete set of the user's watchlist (same length, all owned, no dupes via `validateReorder`), else 400; the reorder runs under the same per-user advisory lock so it can't interleave with adds.
- **`sort_by=title` is wrong** — `order = "media_type, media_id"` (sync.go:781) sorts by type+id, not title; would need a join to media tables.
- **`"unknown"` watched_at semantics undecided** — `"unknown"` in show/season expansion uses `now` rather than the episode `aired_at`; needs a spec decision (aired_at vs now).
- **Bulk-add N+1** — per-item `findSeason`/`findEpisode` queries inside the transaction loop; fine for small batches, quadratic-ish for large syncs.
- **No pagination on `handleGetHistoryItem`** — returns all history rows for one item.
- **Episode rating add/remove payload mismatch (RESOLVED, issue #25)** — `handleAddRatings` declared episodes as `{ids, rating}` (no season/number), so a Trakt episode payload had its `season`/`number` dropped by the decoder, the series TMDB id resolved to the *show*, and the rating was written against the whole series — while `handleRemoveRatings` correctly targeted the episode. Now the episodes payload uses a type carrying `season`/`number`, resolves series→season→episode and writes `media_type='episode'`; a reference without season/number is rejected with 400 `invalid_request` instead of falling back to the show. Regression test: `ratings_test.go` (seeds a series with two episodes, asserts the episode row is rated and the show is not, round-trips `/ratings/remove`, and asserts the 400).
- **No episode support in watchlist/favorites** — Trakt allows episodes/seasons in the watchlist; only movies/shows are handled there. Note this is *symmetric* across add and remove, so it is a feature gap, not the ratings-style add/remove asymmetry. Collection now supports episodes and season/show expansion (issue #71); `/collection/episodes` is served by `collection.go`. **Read-side consequence (issue #4):** watchlist seasons/episodes and collection seasons are still not registered as pinned row variants — see "Per-type reads".
- **Error helpers (`authRequired`/`badRequest`/`internalError`) duplicated** — "match project conventions" per comment, but the shared package exists.
- **Mixed timestamp formats (RESOLVED)** — all response timestamps now unified to `.000Z` via shared `response.FormatTime` (Batch 3); parse paths still accept RFC3339 input.
- **`last_activities` runs many small reads, not one query** — ~24 `SELECT col ... WHERE user_id = ? ORDER BY col DESC LIMIT 1` round trips per call, one per leaf in the mapping table above. Each is index-backed (every source table has a `user_id` index) so they are cheap, but this is a chatty shape; folding it into per-table conditional aggregates (or one `UNION ALL`) is the obvious optimisation if it ever shows up hot. Deliberately not done yet — clarity first, and the issue's own framing is "cheap because the timestamps already exist". (These are read as latest-row reads, not `MAX(col)`, because an aggregate strips the column type and sqlite then returns undecodable text — see the mapping note above.)
- **`last_activities` is uncached by design** — see the freshness note above. It is the one endpoint where a stale cache is actively harmful.

## Todos

- [x] Fix outbox event names on collection/ratings/favorites remove + watchlist reorder (removals/reorder now emit nothing; collection keeps `collection.updated`).
- [x] Return real `added`/`duplicates` counts in watchlist/favorites/ratings add responses.
- [x] Attach `auth.AuthRequired` at route registration (`sync.Use`); per-handler checks dropped (Batch 1).
- [x] Validate `media_type` against the resolved type in `handleGetHistoryItem` (400 on mismatch; `parseWatchedAt` now errors on invalid timestamps instead of silently using `now`).
- [x] Reuse `media.ResolveID` (single-item path) and batch-resolve bulk add IDs via `media.ResolveTMDBBatch` (kills the per-item N+1 in the add loops; Batch 5).
- [x] Advisory-lock rank assignment (watchlist/favorites adds + reorder); reorder is now 1-based and validated as a complete set (400 otherwise).
- [x] Fix `sort_by=title` to join titles. (Batch 10)
- [x] Decide `watched_at` semantics for `"unknown"` (aired_at vs now) and align with spec. (Batch 12 — DECISION: `"unknown"` → *now* (the moment the watch is recorded), matching Trakt's default when no `watched_at` is supplied.) `api-conventions.md` §11 says history `watched_at` accepts the `"released"`/`"unknown"` sentinels (Trakt parity) but does not map them; `sync-api.md` lists the same two sentinels without defining their values. The existing code resolves `"released"` → the item's aired/release date (for calendar-accurate dating of known-released watches) and `"unknown"` → `now` — the same value Trakt uses when a client omits `watched_at` entirely, and the least-surprising choice for an "I don't know when, record it as of the sync" watch. Using `aired_at` for `"unknown"` would backdate every unknown watch to its release date, over-stating history and distorting feeds — a worse default. No code change; current `parseWatchedAt` behavior is correct and now documented as the decided semantics.)
- [x] Use shared `internal/response` error helpers (Batch 0); per-handler `authRequired` is tracked at the route-attachment item above.
- [x] Implement `GET /v1/sync/last_activities` (issue #6, first slice) — Trakt-shaped per-collection timestamps from real aggregates; unsupported fields null, empty sections null. Handler + sqlite tests in `last_activities.go`/`last_activities_test.go`.
- [x] Per-type read variants (issue #4) — 12 GET routes pinning `media_type` over the existing list handlers (`per_type_reads.go`, `per_type_reads_test.go`); unbacked variants (history shows/seasons, watchlist/collection seasons/episodes) left unregistered and documented; `?extended=` narrowing decided against (nothing to narrow). See "Per-type reads".
- [x] Accept the `ids` forms the API serves and the forms Trakt clients send on every sync write (issue #87) — `idsObj.UnmarshalJSON` takes a number or a numeric string; `ids_test.go` covers the decoder, a full serve→echo payload, and a handler-level round trip.
- [ ] Verify module test coverage (`make test`).
