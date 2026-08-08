# Lists Handler (`internal/handlers/lists`)

## What it does

Implements lists, list items, likes, comments, popular/trending, and smart lists (21 OpenAPI routes). This is the largest handler module and the one that most consistently applies the cache/outbox conventions.

**List CRUD**
- `GET /users/{id}/lists` — user's lists, privacy-filtered (public+link_only for non-owners).
- `POST /users/me/lists` — create (slug generated from name + user-id prefix, collision → timestamp suffix).
- `GET /lists/{id}` — by numeric ID **or slug**; 403 for private lists.
- `PUT /lists/{id}` / `DELETE /lists/{id}` — owner-only; advisory-locked cache-tx writes.

**List items**
- `GET /lists/{id}/items` — paginated; sorted per list `sort_by`/`sort_how`; `type` filter; `extended` embeds the list.
- `POST /lists/{id}/items` — add movies/shows/seasons/episodes by TMDB id (resolved via `ExternalID`); reports `added`/`not_found`/`duplicate`.
- `POST /lists/{id}/items/remove` — remove by item ID or by movie/show TMDB ref.
- `PUT /lists/{id}/items/reorder` — assigns rank by array position; marks list ranked.

**Likes / comments / discovery**
- `GET/POST/DELETE /lists/{id}/like` — like_count incremented via `UpdateColumn`; likes list.
- `GET /lists/{id}/comments` — list comments.
- `GET /lists/popular` (period weekly/monthly/all) and `/lists/trending` (like-weight × recency score).

**Smart lists (T4.2)**
- Full CRUD at `/users/me/smart-lists` and `/smart-lists/{id}`; ownership enforced.
- `GET /smart-lists/{id}/items` — resolves the filter DSL (`genres`, `year`, `rating_min`, `votes_min`, `status`, `watched`, `collected`, `type`) against movies/shows tables with user-history subqueries and allowlisted sort columns.

Writes go through `cache.WithCacheTransaction` with list/user version-key bumps and outbox events (`list.created/updated/deleted/items.added/items.removed`).

## Completion

| Piece | Status |
|---|---|
| OpenAPI registration (21 routes) | Done |
| List CRUD with slug lookup + privacy checks | Done |
| Owner-only guards on writes (403) | Done |
| Cache-tx + advisory list lock on writes | Done |
| List/user version-key invalidation | Done |
| Outbox events on list changes | Done |
| Add/remove/reorder items (TMDB resolution, dupes) | Done |
| Like/unlike with counter maintenance | Done |
| Popular (period) + trending | Done |
| Smart list CRUD + ownership | Done |
| Smart list item resolution (filter DSL) | Done |
| Sort-column allowlist (`sanitizeSortColumn`) | Done |

## Oversights

- **`requireAuth` hand-rolled** — duplicate of the pattern in every other package; should be `auth.AuthRequired`. Note the double-check: most write handlers also re-check `uid == ""`.
- **Local error helpers** — full local set (`authRequired`/`badRequest`/`notFound`/`forbidden`/`internalError`) instead of `internal/httpapi`.
- **SQL-injection exposure in sort ordering (RESOLVED)** — `handleGetListItems` did `fmt.Sprintf("%s %s", sortCol, sortDir)` (lists.go:609) with `sortCol` coming straight from the stored `List.SortBy` (no allowlist, unlike smart lists); an owner could store an arbitrary `sort_by` that becomes raw SQL on every items read. New `resolveListItemSort(sortBy, sortHow)` maps sort keys through an allowlist (`rank`, `added_at`, `media_type`, `media_id`, `notes`), falls back to `rank` for unknown keys, and normalizes direction to ASC/DESC (lists.go:598-625). Unit-tested in `internal/handlers/lists/lists_test.go`.
- **Trending score SQL injection** — `handleGetTrendingLists` embeds a fixed expression (safe) but note it's a hardcoded PG expression; acceptable.
- **`generateSlug` userID slicing** — `userID[0:8]` panics if user id is shorter than 8 chars (e.g. empty or short test ids). No length guard.
- **Smart-list pagination is per-type, not global** — each media type queries with its own `OFFSET/LIMIT`, then results are concatenated; global pagination is wrong (offset applies per table, `total_count` is summed). Page 2 may repeat/drop items.
- **Smart-list `total_count` sums across types** while items come from each type's page slice — envelope can report totals that don't match the returned page.
- **`applyFilterDSL` genre filter** — `genres::jsonb ? ?` uses `&&`-style membership per genre but the JSONB operator `?` inside `jsonb` column assumes a JSONB array; confirm column type matches the DSL.
- **`handleAddListItems` closure writes to `result` inside the transaction** — shared mutable state mutated by `processMediaItems` closure; fine since single-threaded, but the `result` is populated even if the tx later fails/rolls back (client sees counts for a rolled-back op → tx returns nil error only on success, so OK, but the non-tx `result.NotFound` appends from a failed resolve are still reported on rollback).
- **`handleLikeList` uses `res.RowsAffected` with `OnConflict{DoNothing}`** — depends on a unique constraint on (list_id, user_id); if the constraint is missing, duplicate likes inflate `like_count`.
- **Comments endpoint not paginated** — `GET /lists/{id}/comments` returns all comments unbounded.
- **Item responses expose raw `database.List`** in several places** (e.g. `resp["list"] = list`, popular/trending return `database.List` directly) — DB models leak to the API without response mapping; inconsistent with list-item mapping elsewhere.

## Todos

- [x] Fix SQL injection in `handleGetListItems` ordering — `resolveListItemSort` allowlist + unit test (lists_test.go).
- [ ] Guard `userID[0:8]` in `generateSlug` against short/empty ids.
- [ ] Fix smart-list pagination to page over the combined result set, not per-type.
- [ ] Paginate `GET /lists/{id}/comments`.
- [ ] Ensure unique constraint exists for list likes; consider a DB-level check.
- [ ] Replace local auth/error helpers with shared packages.
- [ ] Consider response mapping for `database.List` leaks in likes/popular/trending/items-extended.
- [ ] Verify the `genres::jsonb ? ?` filter matches the column type.
- [ ] Verify module test coverage (`make test`).
