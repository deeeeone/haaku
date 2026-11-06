# Comments Handler (`internal/handlers/comments`)

## What it does

Implements the full comment subsystem (17 endpoints):

- **Read**: `GET /comments/trending` (most liked+replied in 7 days), `/recent`, `/updates` (recently edited), `/{id}`, `/{id}/replies`, `/{id}/likes`, `/{id}/reactions`, `/{id}/item` — all auth-optional.
- **Write (auth required)**: `POST /comments` (shout on movie/show/season/episode/list), `PUT /{id}` (own comment), `DELETE /{id}` (own, or moderator/admin; blocked if >14 days old *and* has replies), `POST /{id}/replies` (flat, no nested replies), `POST/DELETE /{id}/like`, `POST/DELETE /{id}/reactions` (6 allowed emoji), `POST /{id}/report` (10 allowed reasons, deduped per user).

Registers OpenAPI metadata for all endpoints. Uses `comment_type`/`media_type` CSV query filters on the listing endpoints and a shared paginated response envelope.

## Media comments by sort (issue #4 §A)

`media_comments.go` adds the four Trakt media-scoped comment lists, all backed by
the existing `comments` table and rendered with this package's `toCommentResponse`
builder:

- `GET /v1/movies/{id}/comments/{sort}`
- `GET /v1/shows/{id}/comments/{sort}`
- `GET /v1/shows/{id}/seasons/{season}/comments/{sort}`
- `GET /v1/shows/{id}/seasons/{season}/episodes/{episode}/comments/{sort}`

The routes are registered in `internal/handlers/media` (which already owns the
movie/show sub-routes), and the shared lister lives here so the response shape
and ordering cannot drift between the four. The nested paths resolve the parent
chain with real lookups and 404 on a missing show/season/episode.

`{sort}` is a closed set — anything else is a 400, not a silent default:

| sort | ORDER BY |
|---|---|
| `newest` | `created_at DESC, id DESC` |
| `oldest` | `created_at ASC, id ASC` |
| `likes` | `like_count DESC, created_at DESC, id DESC` |
| `replies` | `reply_count DESC, created_at DESC, id DESC` |
| `trending` | `(like_count + reply_count) DESC, created_at DESC, id DESC` |

Only top-level comments are listed (`parent_id IS NULL`); replies stay on
`GET /v1/comments/{id}/replies`. Every `comment_type` is included — the sort
segment does not narrow type. The response is a bare JSON array (Trakt's shape)
plus the §7 `X-Pagination-*` headers and `Link: rel="next"`; an empty page is
`[]`, never `null`.

`trending` here orders over all top-level comments on the item; the 7-day
recency window stays on the global `GET /v1/comments/trending`, because hiding a
years-old comment with 500 likes from its own movie page would be wrong.

### Query plans

`likes`, `replies` and `trending` order by counters, not by `created_at`, so
`idx_comments_media (media_type, media_id, created_at DESC)` serves the filter
and the planner sorts. That is acceptable for a per-item page (bounded rows per
media id); a dedicated `(media_type, media_id, like_count)` index is left for
when real traffic justifies it. `newest`/`oldest` are index-only on
`idx_comments_media`.

## Completion

| Piece | Status |
|---|---|
| OpenAPI registration (17 routes) | Done |
| Route wiring incl. auth guards | Done |
| Trending / recent / updates listings | Done |
| Reply flow (flat replies, no nested) | Done |
| Like/unlike with count recalc | Done |
| Reactions with allowlist + count aggregation | Done |
| Reports with reason allowlist + dedupe | Done |
| Edit ownership check + spoiler toggle | Done |
| Delete ownership + moderator override + age/reply guard | Done |
| Item lookup response | Done |

## Oversights

- **`writeError` duplicated** — local envelope helper instead of `internal/httpapi`.
- **Hand-rolled CSV splitter and trimmer (RESOLVED)** — `splitByComma`/`trimSpace` replaced with stdlib `strings.Split`/`strings.TrimSpace` (Batch 4).
- **`user_id` unchecked type assertions** (e.g. comments.go:326) — protected by the shared `auth.AuthRequired()` middleware on write routes, so OK today, but panics if a route is ever mounted without the guard.
- **No reply-count decrement on delete** — deleting a comment that has replies (when allowed, i.e. ≤14 days) leaves the parent's `reply_count` stale; replies themselves are orphaned (no cascade handling).
- **Like/reaction/reply counter increments are non-transactional** — `UpdateColumn("reply_count", +1)` after insert and full `COUNT` recalc on like are separate statements from the write; race-prone. Should use `WithCacheTransaction`.
- **No cache invalidation / outbox events** — comment creates/likes/reactions don't invalidate media/user version keys nor publish events (feeds depend on these).
- **`handleGetLikes` has no pagination** — returns every liker unbounded; violates the "no caps" spirit in the other direction (unbounded payload).
- **Trending uses `like_count + reply_count` as raw column expression** — indexed since migration 0032 (`idx_comments_trending_score`); `reply_count` maintained by app, drift risk.
- **No spoiler/comment length validation** — no max-length cap on `comment` bodies (spec may define one).
- **Delete semantics**: deleting a ≤14-day-old comment with replies succeeds but leaves replies pointing at a deleted parent (rows remain); expected behavior should be confirmed.
- **`db.First(&user, "id = ?", userID)` error ignored** in the moderator check (comments.go:458) — a missing user row silently treats them as non-moderator (safe default but unchecked).

## Todos

- [x] Replace local error helpers with `internal/response` (Batch 0).
- [x] `parseCSV` is stdlib-based (`strings.Split`/`strings.TrimSpace`); the hand-rolled `splitByComma`/`trimSpace` helpers were deleted (Batch 4).
- [ ] Handle reply_count on delete (decrement and/or cascade) and orphaned replies.
- [x] Wrap write flows in `cache.WithCacheTransaction` + cache/version invalidation and outbox events. (Batch 7 — no comment/reaction/reply/report events exist in the webhook catalog, so invalidation only.)
- [x] Paginate `GET /{id}/likes`. (Batch 8 — `?page&limit` default 25/max 100 with §7 `X-Pagination-*` headers + `Link: rel="next"`; stable `created_at DESC, user_id DESC` order.)
- [x] Add comment length validation per spec. (Batch 11)
- [x] Add DB index strategy for trending query (`like_count+reply_count`, `created_at`). (Batch 10 — documented below; the index landed in migration 0032 — see the Databases section for why the requested partial predicate is impossible.)
- [x] Check the user lookup error in the moderator delete path. (Batch 6)
- [x] Verify module test coverage (`make test`). (Batch 14 — `internal/handlers/comments/comments_test.go` adds unit coverage for `validateCommentBody` (400 on empty / >5000-char), the reaction + report-reason allowlists, `parseCSV`, `maxCommentLength`, and `toCommentResponse` (incl. nil-EditedAt). Handler-level integration coverage for the DB-backed create/edit/delete paths awaits a DB-driver approval.)

## Update — media comments by sort (issue #4 §A)

Added `media_comments.go` (`mediaCommentSorts`, `ListMediaComments`) reusing this
package's existing `toCommentResponse` builder; the four media routes are wired
in `internal/handlers/media`. (The builder stays package-private deliberately —
`comments.go` is over the size ratchet's soft limit and may only shrink, so no
line was added to it. See the "Media comments by sort" section above for the sort
table, response shape and query-plan note.)

Coverage: `internal/handlers/media/media_comments_test.go` — sqlite-backed, via
the real `RegisterRoutes` — asserts each sort's exact ordering, the 400 on an
unknown sort, the empty array (not null), pagination edges (page-1 headers +
`Link: rel="next"`, last page has no `Link`, page beyond the end is `[]`, limit
clamp), the missing show/season/episode/movie 404s, the 400 on a non-numeric
season, that replies are excluded, and that the payload carries the full shared
builder shape (`updated_at`/`edited_at`), which the old episodes-local copy
omitted.

## Databases — trending index

`GET /v1/comments/trending` filters to comments created in the last 7 days
(`created_at >= now() - INTERVAL '7 days'`, `comment_type IN (...)`,
`media_type IN (...)`) and orders by `like_count + reply_count DESC,
created_at DESC`. That ordering expression is **not indexable** by the plain
column indexes, so migration 0032 landed the expression index
`idx_comments_trending_score ON comments ((like_count + reply_count) DESC,
created_at DESC)`. Postgres scans it in order and applies the 7-day window as an
`Index Cond` on `created_at`, so the query returns **without a Sort**.

### The partial predicate the issue asked for cannot exist

Issue #2 asked for the index to be *partial*:

```sql
CREATE INDEX idx_comments_trending_score ON comments
    ((like_count + reply_count) DESC, created_at DESC)
    WHERE created_at >= now() - INTERVAL '7 days';
```

That statement cannot run. Postgres requires every function in an index predicate
to be `IMMUTABLE`, and `now()` is only `STABLE`:

```
ERROR:  functions in index predicate must be marked IMMUTABLE
```

So no rolling "last 7 days" window can live in a partial index — the window has
to stay in the query (and it does; the query is unchanged, so results are
identical). Migration 0022 records the same finding for `idx_comments_trending`
(which is why it too carries no predicate). Migration
`20261106213010_comments_trending_score_index_predicate` rebuilds
`idx_comments_trending_score` with its only achievable definition — the exact
expression order, no predicate — and attaches a `COMMENT ON INDEX` recording
this rationale. Migration 0032's file comment claiming "the same 7-day partial
predicate keeps the index small" was wrong.

### Trade-off (accepted)

Without the partial predicate the index covers **all** comments. On a table where
the highest-scoring comments are old, the ordered index scan reads past those
before filling the 7-day window, whereas a partial index would skip them. That
optimization is unachievable for the reason above; keeping the window in the
query, evaluated as an index condition, is the correct and only option.
`comment_type`/`media_type` stay a post-index `Filter` on purpose — the query
needs one global order across the `IN` list, which only the score expression can
provide.
