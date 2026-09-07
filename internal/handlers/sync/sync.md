# Sync Handler (`internal/handlers/sync`)

## What it does

The sync workhorse — 17 endpoints over five collections (all auth-required, per-handler checks):

- **History** (4): GET paginated (+ `type`, `start_at`, `end_at` filters), GET single item, POST add (movies + shows expanded to episodes by season, all-episode expansion), POST remove (by IDs or media objects).
- **Collection** (3): GET, POST add, POST remove (movies/shows).
- **Watchlist** (4): GET (+`sort_by` rank/listed/title), POST add (assigns incrementing rank), POST remove, PUT reorder.
- **Ratings** (3): GET, POST upsert (`OnConflict` on user+media), POST remove.
- **Favorites** (3): GET, POST add (ranked), POST remove.

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
| Cache-tx + version invalidation + outbox | Done |

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
- **No episode support in collection/watchlist/favorites** — Trakt allows episodes in watchlist; only movies/shows handled here.
- **Error helpers (`authRequired`/`badRequest`/`internalError`) duplicated** — "match project conventions" per comment, but the shared package exists.
- **Mixed timestamp formats (RESOLVED)** — all response timestamps now unified to `.000Z` via shared `response.FormatTime` (Batch 3); parse paths still accept RFC3339 input.

## Todos

- [x] Fix outbox event names on collection/ratings/favorites remove + watchlist reorder (removals/reorder now emit nothing; collection keeps `collection.updated`).
- [x] Return real `added`/`duplicates` counts in watchlist/favorites/ratings add responses.
- [x] Attach `auth.AuthRequired` at route registration (`sync.Use`); per-handler checks dropped (Batch 1).
- [x] Validate `media_type` against the resolved type in `handleGetHistoryItem` (400 on mismatch; `parseWatchedAt` now errors on invalid timestamps instead of silently using `now`).
- [x] Reuse `media.ResolveID` (single-item path) and batch-resolve bulk add IDs via `media.ResolveTMDBBatch` (kills the per-item N+1 in the add loops; Batch 5).
- [x] Advisory-lock rank assignment (watchlist/favorites adds + reorder); reorder is now 1-based and validated as a complete set (400 otherwise).
- [x] Fix `sort_by=title` to join titles. (Batch 10)
- [ ] Decide `watched_at` semantics for `"unknown"` (aired_at vs now) and align with spec.
- [x] Use shared `internal/response` error helpers (Batch 0); per-handler `authRequired` is tracked at the route-attachment item above.
- [ ] Verify module test coverage (`make test`).
