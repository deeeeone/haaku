# Filters Handler (`internal/handlers/filters`)

## What it does

Implements saved-filter persistence for the current user:

- `GET /v1/users/me/saved_filters/{section}` — list the user's saved filters for a section, ordered `created_at ASC`.
- `POST /v1/users/me/saved_filters` — save a filter (section + name + arbitrary JSON filter), max **20 per user per section** (400 when exceeded).
- `DELETE /v1/users/me/saved_filters/{id}` — delete own filter (owner checked; non-owners get 403).

All routes are auth-required (shared `auth.AuthRequired()` middleware). Registers OpenAPI metadata for all three endpoints.

## Completion

| Piece | Status |
|---|---|
| OpenAPI registration (3 routes) | Done |
| List filters by section | Done |
| Save filter with 20-per-section cap | Done |
| Delete filter with ownership check | Done |
| Auth guard on all routes | Done |

## Oversights

- **`requireAuth` hand-rolled (RESOLVED)** — routes now run shared `auth.AuthRequired()` (Batch 1).
- **Local error helpers (RESOLVED)** — `writeError`/`badRequest`/`notFound` duplicate `internal/httpapi`.
- **Hand-formatted timestamp (RESOLVED)** — `toFilterResponse` now emits via shared `response.FormatTime` (`.000Z`; Batch 3).
- **Owner check returns 404 instead of 403 (RESOLVED)** — deleting someone else's filter now returns 403 `forbidden`, aligned with api-conventions (authenticated but not allowed).
- **No `section` allowlist validation** — any arbitrary section string is accepted on save; typo'd sections silently create dead entries. Spec should define allowed sections.
- **No duplicate-name guard** — same section+name can be saved repeatedly.
- **Count cap is check-then-insert** — race allows exceeding 20 without a constraint/advisory lock; acceptable for personal data but not airtight.
- **No cache invalidation** — saved-filter changes don't touch version keys; minor since these are user-scoped reads.
- **Plain writes** — no `WithCacheTransaction` (per conventions for DB writes).

## Todos

- [x] Swap `requireAuth` for shared `auth.AuthRequired` (Batch 1).
- [x] Use `internal/response` error constructors (Batch 0).
- [x] Use shared `response.FormatTime` instead of the hand-rolled timestamp literal.
- [ ] Validate section against a defined allowlist (spec).
- [x] Decide 403-vs-404 for cross-user delete; align with conventions. (Batch 6)
- [ ] Optionally enforce uniqueness on (user, section, name) and harden the 20-cap.
- [x] Route writes through `WithCacheTransaction`. (Batch 7)
- [ ] Verify module test coverage (`make test`).
