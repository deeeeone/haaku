# Activity Handler (`internal/handlers/activity`)

## What it does

Implements the social activity feed endpoint:

- `GET /v1/users/me/activity` — returns recent activity events from the current user's social graph.

The feed is filtered by a `type` query param:
- `following` — activity from users the current user follows.
- `followers` — activity from users who follow the current user.
- `friends` (default) — activity from mutual follows (both follow each other).
- No graph matches → returns an empty paginated payload.

The handler:
1. Resolves the actor ID set from the `UserFollow` graph based on `type`.
2. Optionally filters by `start_at` (RFC3339) cursor.
3. Queries `ActivityFeed` rows for those actors, ordered `created_at DESC`, paginated via `page`/`limit`.
4. Returns a paginated envelope (`data`, `page`, `limit`, `total_count`).

Also registers OpenAPI route metadata (via `openapi.Register`) and exposes `RegisterRoutes` to wire the route group. The route runs the shared `auth.AuthRequired()` middleware; response types and error/pagination helpers come from shared packages.

## Completion

| Piece | Status |
|---|---|
| Route registration in OpenAPI registry | Done |
| Fiber route wiring (`/users/me/activity`) | Done |
| Auth guard (user_id from Locals) | Done |
| Feed types: following / followers / friends | Done |
| `start_at` cursor filter | Done |
| Pagination (`page`, `limit` clamped 1–100) | Done |
| Empty-graph fast path | Done |
| Error envelope for DB-unavailable / auth | Done |

## Oversights

- **Duplicated error helper** — `writeError` re-implements the envelope inline instead of reusing `internal/httpapi` helpers, and it drops the shared `httpapi.NotFound/BadRequest/Unauthorized` constructors. If the envelope spec changes, this copy can drift.
- **Bare `requireAuth` (RESOLVED)** — route now runs the shared `auth.AuthRequired()` (Batch 1); the local middleware was removed.
- **No trailing-page handling** — `page` beyond the last page returns empty `data` with the same `total_count`; no explicit `X-Pagination-Page-Count` style metadata (spec-dependent).
- **`start_at` parse failure silently ignored** — an invalid `start_at` is treated as absent rather than returning a 400.
- **`friends`/`type` validation** — unknown `type` values silently fall through to the `friends` branch; no 400 for invalid input.
- **No default `type` in OpenAPI doc** — query param not described in the route definition.

## Todos

- [x] Switch to shared `internal/response` error constructors (local `writeError` removed).
- [x] Move auth guard to a shared middleware package — route now runs `auth.AuthRequired()` (Batch 1).
- [x] Return 400 on malformed `start_at` and on unknown `type` values. (Batch 6)
- [x] Confirm pagination metadata contract against `api-conventions.md` and align response shape. (Batch 8 — body envelope kept; now emits the §7 `X-Pagination-*` headers + `Link: rel="next"` and a stable `created_at DESC, id DESC` order; default limit aligned to §7's 25.)
- [ ] Verify the module has tests (`make test` TDD requirement).
