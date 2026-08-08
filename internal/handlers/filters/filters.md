# Filters Handler (`internal/handlers/filters`)

## What it does

Implements saved-filter persistence for the current user:

- `GET /v1/users/me/saved_filters/{section}` — list the user's saved filters for a section, ordered `created_at ASC`.
- `POST /v1/users/me/saved_filters` — save a filter (section + name + arbitrary JSON filter), max **20 per user per section** (400 when exceeded).
- `DELETE /v1/users/me/saved_filters/{id}` — delete own filter (owner checked; non-owners get 404).

All routes are auth-required (hand-rolled `requireAuth`). Registers OpenAPI metadata for all three endpoints.

## Completion

| Piece | Status |
|---|---|
| OpenAPI registration (3 routes) | Done |
| List filters by section | Done |
| Save filter with 20-per-section cap | Done |
| Delete filter with ownership check | Done |
| Auth guard on all routes | Done |

## Oversights

- **`requireAuth` hand-rolled** — same Local check as other packages; should be the shared `auth.AuthRequired` middleware.
- **Local error helpers** — `writeError`/`badRequest`/`notFound` duplicate `internal/httpapi`.
- **Hand-formatted timestamp** — `"2006-01-02T15:04:05.000Z"` literal in `toFilterResponse`.
- **Owner check returns 404 instead of 403** — deleting someone else's filter masquerades as "not found"; spec may want 403 (info-disclosure tradeoff, but note it).
- **No `section` allowlist validation** — any arbitrary section string is accepted on save; typo'd sections silently create dead entries. Spec should define allowed sections.
- **No duplicate-name guard** — same section+name can be saved repeatedly.
- **Count cap is check-then-insert** — race allows exceeding 20 without a constraint/advisory lock; acceptable for personal data but not airtight.
- **No cache invalidation** — saved-filter changes don't touch version keys; minor since these are user-scoped reads.
- **Plain writes** — no `WithCacheTransaction` (per conventions for DB writes).

## Todos

- [ ] Swap `requireAuth` for shared `auth.AuthRequired`.
- [ ] Use `internal/httpapi` error constructors + `time.RFC3339` formatting.
- [ ] Validate section against a defined allowlist (spec).
- [ ] Decide 403-vs-404 for cross-user delete; align with conventions.
- [ ] Optionally enforce uniqueness on (user, section, name) and harden the 20-cap.
- [ ] Route writes through `WithCacheTransaction`.
- [ ] Verify module test coverage (`make test`).
