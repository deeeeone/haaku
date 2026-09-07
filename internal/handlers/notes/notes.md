# Notes Handler (`internal/handlers/notes`)

## What it does

Implements personal notes on media items:

- `POST /v1/notes` — upsert a note (one per user+media_type+media_id); create or update, returns 200 in both cases.
- `GET /v1/notes/{id}` — get own note by id (owner-only; others get 403).
- `PUT /v1/notes/{id}` — update own note body (owner-only; 403 for others).
- `DELETE /v1/notes/{id}` — delete own note (owner-only; 403 for others), 204.
- `GET /v1/users/me/notes` — list own notes, paginated (default 20, max 100), optional `media_type` filter.

All routes are auth-required via the shared `auth.AuthRequired()` middleware. Registers OpenAPI metadata for all five endpoints.

## Completion

| Piece | Status |
|---|---|
| OpenAPI registration (5 routes) | Done |
| Upsert create/update note | Done |
| Get/update/delete owner-scoped | Done |
| List with pagination + media_type filter | Done |
| Auth guard on all routes | Done |

## Oversights

- **`requireAuth` hand-rolled (RESOLVED)** — routes now run shared `auth.AuthRequired()` (Batch 1).
- **Local error helpers + duplicated pagination helpers (RESOLVED)** — `writeError`/`badRequest`/`notFound`/`queryInt`/`clampLimit` reimplemented again.
- **Upsert is check-then-write, not atomic** — `First` then `Create`/`Save` has a race window (two concurrent POSTs for the same media can produce a unique-violation or a lost update). Should use `clause.OnConflict` upsert and/or advisory lock; DB unique constraint required.
- **Owner-elsewhere now returns 403 (not 404)** — GET/PUT/DELETE fetch the note by id first; a genuinely missing note is 404 `not_found`, while a note owned by another user is 403 `forbidden`, matching the OpenAPI contract.
- **No note length limit** — unbounded body accepted.
- **`media_type` not validated** — any string accepted; no allowlist (movie/show/season/episode).
- **Hand-formatted timestamps (RESOLVED)** — now emitted via shared `response.FormatTime` (`.000Z`; Batch 3).
- **No cache invalidation / outbox events** — notes changes don't bump user version keys (notes aren't cached today, so low impact, but inconsistent with conventions for writes).
- **POST returns 200 (not 201)** — documented as 200; create-vs-update is indistinguishable to clients. Confirm spec intent.

## Todos

- [x] Swap `requireAuth` for shared `auth.AuthRequired` (Batch 1).
- [ ] Make the upsert atomic (DB unique constraint + `OnConflict` upsert) or advisory-locked.
- [x] Reconcile 403-vs-404 for cross-user access against the spec/docs. (Batch 6)
- [ ] Add note-length and `media_type` validation.
- [x] Replace local error/pagination helpers with `internal/response` (Batch 0).
- [ ] Route writes through `WithCacheTransaction` per conventions.
- [ ] Verify module test coverage (`make test`).
