# Comments Handler (`internal/handlers/comments`)

## What it does

Implements the full comment subsystem (17 endpoints):

- **Read**: `GET /comments/trending` (most liked+replied in 7 days), `/recent`, `/updates` (recently edited), `/{id}`, `/{id}/replies`, `/{id}/likes`, `/{id}/reactions`, `/{id}/item` — all auth-optional.
- **Write (auth required)**: `POST /comments` (shout on movie/show/season/episode/list), `PUT /{id}` (own comment), `DELETE /{id}` (own, or moderator/admin; blocked if >14 days old *and* has replies), `POST /{id}/replies` (flat, no nested replies), `POST/DELETE /{id}/like`, `POST/DELETE /{id}/reactions` (6 allowed emoji), `POST /{id}/report` (10 allowed reasons, deduped per user).

Registers OpenAPI metadata for all endpoints. Uses `comment_type`/`media_type` CSV query filters on the listing endpoints and a shared paginated response envelope.

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
- **Trending uses `like_count + reply_count` as raw column expression** — portable enough in PG but unindexed; `reply_count` maintained by app, drift risk.
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
- [x] Add DB index strategy for trending query (`like_count+reply_count`, `created_at`). (Batch 10 — documented in the Databases section below; no schema change landed here per project rules.)
- [x] Check the user lookup error in the moderator delete path. (Batch 6)
- [x] Verify module test coverage (`make test`). (Batch 14 — `internal/handlers/comments/comments_test.go` adds unit coverage for `validateCommentBody` (400 on empty / >5000-char), the reaction + report-reason allowlists, `parseCSV`, `maxCommentLength`, and `toCommentResponse` (incl. nil-EditedAt). Handler-level integration coverage for the DB-backed create/edit/delete paths awaits a DB-driver approval.)

## Databases — trending index (migration needed, out of scope here)

`GET /v1/comments/trending` filters to comments created in the last 7 days
(`created_at >= now() - INTERVAL '7 days'`, `comment_type IN (...)`,
`media_type IN (...)`) and orders by `like_count + reply_count DESC,
created_at DESC`. That ordering expression is **not indexable** by the existing
indexes — migration 0022 creates `idx_comments_trending ON comments
(comment_type, media_type, created_at DESC) WHERE created_at >= now() -
INTERVAL '7 days'`, which serves the WHERE clause but cannot accelerate an
`ORDER BY (like_count + reply_count)` because it is a computed expression.

Per project rules no schema change (new migration / `ALTER TABLE`) was made in
Batch 10. The query was left unchanged to preserve identical results. When a
schema change is next permitted, add an expression-index matching the exact
query shape — same partial predicate + the score expression + `created_at`
tiebreak:

```sql
CREATE INDEX idx_comments_trending_score ON comments
    ((like_count + reply_count) DESC, created_at DESC)
    WHERE created_at >= now() - INTERVAL '7 days';
```

Notes:
- **When to create:** in a new append-only migration, coordinated with the
  schema-doc update in `../trakt-killer-plan/02-architecture/data-model.md`
  (rule: update the planning data-model BEFORE the migration). Not in this
  batch — schema changes are disallowed here and no migration may be edited.
- **Why this shape:** the partial predicate exactly mirrors the handler's 7-day
  window so the index stays small; the score expression + `created_at` order
  matches `ORDER BY` so Postgres can fetch in index order without a sort, then
  the `comment_type`/`media_type` filter runs on the small post-window index.
- **Alternative evergreen index** (covers both trending and a plain
  like-count sort if the spec ever drops the 7-day window): create the same
  expression index *without* the partial predicate and rely on the existing
  `idx_comments_type_created` for the WHERE.
