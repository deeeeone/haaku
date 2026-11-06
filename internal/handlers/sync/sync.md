# Sync Handler (`internal/handlers/sync`)

## What it does

The sync workhorse — 18 endpoints over five collections plus the last-activities delta (all auth-required, per-handler checks):

- **History** (4): GET paginated (+ `type`, `start_at`, `end_at` filters), GET single item, POST add (movies + shows expanded to episodes by season, all-episode expansion), POST remove (by IDs or media objects).
- **Collection** (3): GET, POST add, POST remove (movies/shows).
- **Watchlist** (4): GET (+`sort_by` rank/listed/title), POST add (assigns incrementing rank), POST remove, PUT reorder.
- **Ratings** (3): GET, POST upsert (`OnConflict` on user+media), POST remove. Episode ratings carry `season`/`number` and resolve series→season→episode, exactly as history add does; an episode reference without them is rejected (400).
- **Favorites** (3): GET, POST add (ranked), POST remove.
- **Last activities** (1): GET `/v1/sync/last_activities` — Trakt's "what changed since" delta, per-collection timestamps sourced from the rows we already write (issue #6). See "Last activities" below.

Writes use `cache.WithCacheTransaction` with version-counter invalidation (`queueUserInvalidation`/`queueMediaInvalidation`/`queueEpisodeInvalidations`) and outbox events (`EventHistoryAdded`, `EventCollectionAdded`, `EventWatchlistAdded`, `EventRatingAdded`, `EventFavoriteAdded`). Batch responses follow the Trakt `added/not_found/duplicates` shape.

## Completion

| Piece | Status |
|---|---|
| OpenAPI registration (17 routes) | Done |
| History GET/add/remove (show→episode expansion) | Done |
| Collection GET/add/remove | Done |
| Watchlist GET/add/remove/reorder | Done |
| Ratings GET/upsert/remove | Done |
| Favorites GET/add/remove | Done |
| Last activities (GET `/v1/sync/last_activities`) | Done (issue #6) — real per-collection aggregates, no cache |
| Cache-tx + version invalidation + outbox | Done |

## Last activities (`GET /v1/sync/last_activities`)

The delta endpoint every Trakt client polls before syncing: cache the timestamps
locally and only re-fetch a collection whose timestamp moved. The response
mirrors Trakt's `last_activities` shape — a section per collection, each an
object of Trakt's timestamp field names; a section the caller has no rows for is
`null` (never an object of nulls or a placeholder `now`). `all` is the greatest
activity across every section.

Every leaf is a real `MAX(...)` aggregate over the owning table, scoped to the
caller (`handlers/sync/last_activities.go`):

| Section | Field | Source (`MAX(col)` where `user_id = caller`) |
|---|---|---|
| `movies` | `watched_at` / `collected_at` / `rated_at` / `watchlisted_at` / `favorited_at` / `commented_at` / `paused_at` / `hidden_at` | `user_history.watched_at` / `user_collection.collected_at` / `user_ratings.rated_at` / `user_watchlist.listed_at` / `user_favorites.favorited_at` / `comments.created_at` / `user_playback.paused_at` / `user_hidden.hidden_at` (all `media_type='movie'`) |
| `episodes` | `watched_at` / `rated_at` / `commented_at` / `paused_at` | same tables, `media_type='episode'` |
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
invented): `episodes.collected_at`, `episodes.watchlisted_at`,
`seasons.watchlisted_at` — our `user_collection`/`user_watchlist` only carry
`movie`/`show` (see the collection/watchlist gap below). Trakt's top-level
`recommendations`, `account`, and `saved_filters` sections are not part of our
response at all (we store no per-user activity time for them).

**Freshness / caching decision: per-request aggregates, no cache.** Each request
runs a bounded set of indexed `MAX()` aggregates over that one user's rows
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

- **WRONG OUTBOX EVENT NAMES on removals/reorder (RESOLVED)** — copy-paste bugs:
  - `handleRemoveCollection` publishes `EventCollectionAdded` with `{"deleted": ...}` (sync.go:737) — **kept as-is**: the webhook catalog defines `collection.updated` as the add+remove event, so this is the correct constant (comment added).
  - `handleRemoveRatings` published `EventRatingAdded`, `handleRemoveFavorites` published `EventFavoriteAdded`, `handleReorderWatchlist` published `EventWatchlistAdded` (sync.go) — **fixed**: the webhook catalog (`trakt-killer-plan/03-api/webhooks.md`) defines no rating/favorite removal or watchlist-reorder events, so these handlers now emit nothing.
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
- **No episode support in collection/watchlist/favorites** — Trakt allows episodes in watchlist; only movies/shows handled here. Note this is *symmetric* across add and remove (both accept only movies/shows), so it is a feature gap, not the ratings-style add/remove asymmetry. Tracked on haaku-tv/haaku#4.
- **Error helpers (`authRequired`/`badRequest`/`internalError`) duplicated** — "match project conventions" per comment, but the shared package exists.
- **Mixed timestamp formats (RESOLVED)** — all response timestamps now unified to `.000Z` via shared `response.FormatTime` (Batch 3); parse paths still accept RFC3339 input.
- **`last_activities` runs many small aggregates, not one query** — ~24 `SELECT MAX(col) ... WHERE user_id = ?` round trips per call, one per leaf in the mapping table above. Each is index-backed (every source table has a `user_id` index) so they are cheap, but this is a chatty shape; folding it into per-table conditional aggregates (or one `UNION ALL`) is the obvious optimisation if it ever shows up hot. Deliberately not done yet — clarity first, and the issue's own framing is "cheap because the timestamps already exist".
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
- [ ] Verify module test coverage (`make test`).
