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
- **Hand-rolled CSV splitter and trimmer** — `splitByComma`/`trimSpace` reimplement `strings.Split`/`strings.TrimSpace`.
- **`user_id` unchecked type assertions** (e.g. comments.go:326) — protected by `requireAuth` on write routes, so OK today, but panics if a route is ever mounted without the guard.
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
- [ ] Consolidate local CSV helpers (`parseCSV`/`splitByComma`/`trimSpace`) into stdlib-based shared helpers.
- [ ] Handle reply_count on delete (decrement and/or cascade) and orphaned replies.
- [ ] Wrap write flows in `cache.WithCacheTransaction` + cache/version invalidation and outbox events.
- [ ] Paginate `GET /{id}/likes`.
- [ ] Add comment length validation per spec.
- [ ] Add DB index strategy for trending query (`like_count+reply_count`, `created_at`).
- [ ] Check the user lookup error in the moderator delete path.
- [ ] Verify module test coverage (`make test`).
