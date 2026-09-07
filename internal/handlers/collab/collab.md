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

- **No auth middleware on any route (RESOLVED)** — collab routes were registered bare with no global auth layer, so the endpoints were effectively unauthenticated. All 4 routes now run `auth.AuthRequired()`.
- **Unchecked type assertions on `user_id` (RESOLVED)** — `c.Locals("user_id").(string)` (collab.go:112, 146, 221) without the `ok` form panicked → 500 via Recovery when the Local was unset. All handlers now use the checked form and return 401.
- **`GET /users/me/lists/collaborations` claim mismatch** — comment says "owned + collaborated" but the query only returns `ListCollaborator` rows (collaborated), not lists the user owns. Spec check needed.
- **Duplicated error helpers** — `writeError`/`badRequest`/`notFound` reimplement the envelope; should use `internal/httpapi`.
- **Timestamps hand-formatted (RESOLVED)** — now emitted via shared `response.FormatTime` (`.000Z` millisecond precision; Batch 3).
- **No invite acceptance flow** — `AcceptedAt` is stored/returned but no endpoint ever sets it (no accept/decline endpoint, no notification on invite).
- **Owner lookup treats 404 as "list not found"** — a list owned by someone else returns `not_found` instead of `forbidden`. Spec distinguishes 403 for non-owners.
- **Raw `parseID` (RESOLVED)** — now `strconv.ParseInt` (returns 0 on non-numeric; Batch 4).
- **Plain writes, no cache-tx/advisory lock** — collab changes don't invalidate list cache version keys (conventions require `WithCacheTransaction` + version counters).

## Todos

- [x] Add `auth.AuthRequired` middleware to all collab routes; use checked `user_id` asserts (401 on missing).
- [x] Decide owned+collaborated vs collaborated-only semantics for `GET /users/me/lists/collaborations`. (Batch 12 — DECISION: spec = owned + collaborated.) `lists-api.md` defines this route as "all lists the user can edit (owned + collaborated)", which is unambiguous: a user can edit the lists they own AND the lists they're a collaborator on. The current implementation returns only `list_collaborators` membership rows (collaborated-only), omitting lists the user owns — a divergence from the spec. Implementing the owned portion requires returning actual `lists` rows (the current response is a membership shape with no `role`/`invited_at` meaning for owned lists), and the spec does not pin the response schema for this route. RECORDED: the correct semantics per spec are owned+collaborated; a full implementation that returns owned lists is tracked as pending (needs a defined response shape — full list objects vs the current membership rows) and is not guessed into here.)
- [x] Implement the invite accept/decline flow or explicitly document `AcceptedAt` as future. (Batch 12 — DECISION: no accept/decline flow; invites are immediate.) `lists-api.md` defines only four collaboration routes — GET collaborations, POST invite, DELETE remove, GET list members — and **no accept/decline endpoint**. Inviting a user adds them as an `editor`/`viewer` collaborator directly (they can edit immediately). `accepted_at` (which exists in migration 0020 but is not mentioned in `data-model.md`'s `list_collaborators` row) is therefore recorded as **future work** — unused by any current spec route, retained only for a later join-by-approval flow (lists-api "Phase 2: join-by-link with approval"). No endpoint is added because the spec defines none; behavior (immediate collaboration on invite) already matches the spec.)
- [x] Return 403 (not 404) when the list exists but the caller isn't the owner. (Batch 6)
- [x] Route writes through `cache.WithCacheTransaction` with list version-key invalidation. (Batch 7)
- [x] Replace local error helpers with shared `internal/response` (Batch 0).
- [x] Consolidate the identical fiber.Ctx `parseID` copies in episodes/seasons/comments into a shared helper (`response.ParseIDParam`; collab's string `parseID` still uses `strconv.ParseInt`; Batch 5).
- [ ] Verify module test coverage (`make test`).
