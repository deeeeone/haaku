# Social Handler (`internal/handlers/social`)

## What it does

Follow graph + approval flow. Eight routes:

- `POST /v1/users/:id/follow` — follow; private target → pending (`ApprovedAt` nil), else approved immediately. Idempotent (already-following returns 201 with current status).
- `DELETE /v1/users/:id/follow` — unfollow (204 / 404).
- `GET /v1/users/:id/followers|following|friends` — lists; friends = mutual approved follows (self-join on `user_follows`).
- `GET /v1/users/me/follow_requests` — pending incoming requests.
- `POST /v1/users/me/follow_requests/:follower_id/approve` — sets `approved_at`.
- `DELETE /v1/users/me/follow_requests/:follower_id` — deny (delete row).

Emits outbox events (`user.followed`, `user.unfollowed`, `user.follow_approved`). Reuses `database.UserFollow`/`User` models and `resolveUserStubs` for bulk profile resolution.

## Completion

| Piece | Status |
|---|---|
| OpenAPI registration (8 routes) | Done |
| Follow/unfollow (private→pending) | Done |
| Followers/following/friends lists | Done |
| Follow request list/approve/deny | Done |
| Outbox events on mutations | Done |

## Oversights

- **CRITICAL: no auth middleware on any route (RESOLVED)** — `RegisterRoutes` attached no `requireAuth`, and every handler read `c.Locals("user_id").(string)` with an **unchecked type assertion** (social.go:88, 151, 261, 303, 334), so an unauthenticated request hit `nil` → panic on the assert → 500 via Recovery, not 401. Now the 5 auth-required endpoints (follow, unfollow, follow_requests, approve, deny) run `auth.AuthRequired()` middleware, the 3 public read routes (followers/following/friends) stay unauthenticated per OpenAPI, and every handler uses the checked form returning 401 `unauthorized` when the user is missing.
- **No `cache.WithCacheTransaction`, no advisory locks** — mutations run on plain `db` (Create/Update/Delete) with no transaction/cache invalidation. Outbox events published directly with `db`, not within the write transaction (compare `checkin`/`scrobble`).
- **Raw event-name strings** — `"user.followed"`, `"user.unfollowed"`, `"user.follow_approved"` are literals, not outbox constants (checkin uses `outbox.EventCheckinCreated`). Drift/typo risk; verify these names exist in `internal/outbox`.
- **No `notifications.CreateNotification`** — approval/follow events are outboxed but no notification record is created for the target (spec likely wants inbox notifications).
- **Unchecked errors throughout** — `listFollowRelations` ignores `Count` error; `resolveUserStubs` ignores `Find` error; several handlers ignore `db.Exec`/`Find` errors and return empty lists on failure.
- **No pagination** — followers/following/friends/requests are unbounded lists (charter: unlimited data → must paginate).
- **No ordering** — `Find` without `Order` on follower/following lists; order is undefined.
- **`handleUnfollow` doesn't guard self-follow** (`targetID == followerID`) while `handleFollow` does.
- **`handleFollow` returns 201 for already-following** — arguably should be 200 or an idempotent 201; at least it diverges from REST semantics.
- **Local error helpers** (`writeError`/`badRequest`/`notFound`) duplicated from `internal/httpapi`; hand-rolled timestamp literals.
- **No rate limiting / abuse control** on follow spam (429 per charter).

## Todos

- [x] Attach `auth.AuthRequired` to all mutation routes; drop unchecked asserts (fail closed with 401). Public reads (followers/following/friends) stay open.
- [ ] Wrap follow/unfollow/approve/deny in `cache.WithCacheTransaction` with version-counter invalidation and in-tx outbox publish.
- [ ] Define typed outbox constants for follow events (or reuse existing ones).
- [ ] Create inbox notifications on follow/approve.
- [ ] Add pagination + stable ordering to all list endpoints.
- [ ] Handle all DB errors instead of ignoring them.
- [ ] Use shared `internal/httpapi` helpers.
- [ ] Verify module test coverage (`make test`).
