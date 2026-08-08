# Collab Handler (`internal/handlers/collab`)

## What it does

Implements list collaboration endpoints:

- `GET /v1/users/me/lists/collaborations` — returns list rows where the current user is a collaborator (owned + collaborated).
- `POST /v1/lists/{id}/collaborators` — invites a user (by username) as `editor` or `viewer`; owner only, 201 on success.
- `DELETE /v1/lists/{id}/collaborators/{user_id}` — removes a collaborator; owner only, 204.
- `GET /v1/lists/{id}/collaborators` — lists collaborators for a list.

Ownership is enforced by checking `lists.id = ? AND lists.user_id = ?`. Registers OpenAPI metadata for all four endpoints.

## Completion

| Piece | Status |
|---|---|
| OpenAPI registration (4 routes) | Done |
| Route wiring | Done |
| Owner-only guard for invite/remove | Done |
| Username → user ID resolution | Done |
| Self-invite rejection | Done |
| Duplicate collaborator rejection | Done |
| Role validation (editor/viewer) | Done |
| 204 / 201 / 404 semantics | Done |

## Oversights

- **No auth middleware on any route** — unlike `checkin` (uses `auth.AuthRequired`), collab routes are registered bare. `server.go` has no global auth middleware, so these endpoints are effectively **unauthenticated**.
- **Unchecked type assertions on `user_id`** — `c.Locals("user_id").(string)` (collab.go:112) without the `ok` form panics when the Local is unset → 500 via Recovery. All four handlers do this.
- **`GET /users/me/lists/collaborations` claim mismatch** — comment says "owned + collaborated" but the query only returns `ListCollaborator` rows (collaborated), not lists the user owns. Spec check needed.
- **Duplicated error helpers** — `writeError`/`badRequest`/`notFound` reimplement the envelope; should use `internal/httpapi`.
- **Timestamps hand-formatted** — `"2006-01-02T15:04:05.000Z"` string literals duplicated in three handlers instead of a shared `time.Format(time.RFC3339)` helper.
- **No invite acceptance flow** — `AcceptedAt` is stored/returned but no endpoint ever sets it (no accept/decline endpoint, no notification on invite).
- **Owner lookup treats 404 as "list not found"** — a list owned by someone else returns `not_found` instead of `forbidden`. Spec distinguishes 403 for non-owners.
- **Raw `parseID`** — hand-rolled numeric parser instead of `strconv.ParseInt`.
- **Plain writes, no cache-tx/advisory lock** — collab changes don't invalidate list cache version keys (conventions require `WithCacheTransaction` + version counters).

## Todos

- [ ] Add `auth.AuthRequired` middleware to all collab routes.
- [ ] Use the `ok` form of type assertions (or shared `getUserID` helper) to avoid panics.
- [ ] Decide owned+collaborated vs collaborated-only semantics for `GET /users/me/lists/collaborations`.
- [ ] Implement the invite accept/decline flow or explicitly document `AcceptedAt` as future.
- [ ] Return 403 (not 404) when the list exists but the caller isn't the owner.
- [ ] Route writes through `cache.WithCacheTransaction` with list version-key invalidation.
- [ ] Replace local error/timestamp/parse helpers with shared `internal/httpapi` + stdlib.
- [ ] Verify module test coverage (`make test`).
