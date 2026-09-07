# Hidden Handler (`internal/handlers/hidden`)

## What it does

Implements the hidden-items subsystem:

- `GET /v1/users/hidden/{section}` — list the user's hidden items in a section (ordered `hidden_at DESC`).
- `POST /v1/users/hidden/{section}` — hide an item (idempotent via `FirstOrCreate`), 201.
- `DELETE /v1/users/hidden/{section}` — unhide an item, 204 (404 if not currently hidden).

Section allowlist enforced server-side: `calendar`, `progress_watched`, `progress_collected`, `recommendations`, `comments`, `dropped`. Uses the **shared `auth.AuthRequired()`** middleware. Registers OpenAPI metadata for all three endpoints.

## Completion

| Piece | Status |
|---|---|
| OpenAPI registration (3 routes) | Done |
| Shared `auth.AuthRequired` middleware | Done |
| Section allowlist validation | Done |
| List hidden items | Done |
| Add hidden (idempotent) | Done |
| Remove hidden with 404 semantics | Done |
| Request validation (media_type/media_id required) | Done |

## Oversights

- **Local error helpers** — `writeError`/`badRequest`/`notFound` duplicate `internal/httpapi`.
- **Dead code at hidden.go:188-189** — `_ = db` with a "Publish outbox event (best-effort)" comment that does nothing. Either implement the outbox publish (hiding affects calendar/progress/recommendations feeds) or remove the stub.
- **No cache invalidation** — hiding/unhiding doesn't invalidate user version keys, yet the calendar handler reads `user_hidden` to exclude items; cached feeds can go stale.
- **`FirstOrCreate` doesn't refresh timestamps** — re-hiding an already-hidden item returns 201 with the original `HiddenAt` (idempotent, but semantically 200-ish); behavior should be confirmed.
- **Hand-formatted timestamps (RESOLVED)** — now emitted via shared `response.FormatTime` (`.000Z`; Batch 3).
- **No `media_type` allowlist** — arbitrary strings accepted; a typo'd `media_type` creates unreachable hidden rows (and can silently break exclusion joins downstream).
- **Plain writes** — no `WithCacheTransaction` / advisory locks; fine for personal data but inconsistent with conventions.

## Todos

- [x] Implement or remove the outbox-event stub. (Batch 7 — removed; the webhook catalog defines no hide/unhide event)
- [x] Invalidate user/feed version keys on hide/unhide. (Batch 7)
- [x] Use `internal/response` error constructors (Batch 0).
- [x] Use shared `response.FormatTime` instead of the hand-rolled timestamp literal.
- [x] Validate `media_type` against the spec's allowed values. (Batch 11)
- [x] Confirm 201-vs-200 semantics for idempotent re-hide. (Batch 12 — DECISION: keep `201` for every successful hide, including an idempotent re-hide.) `sync-api.md` lists `POST /users/:id/hidden/:section` with no per-status contract, and our OpenAPI route (source of truth) documents **201** for POST. `FirstOrCreate` makes re-hiding idempotent (no duplicate row, `HiddenAt` preserved), but the request's effect — "this item is hidden in this section" — holds whether the row was just created or already existed, so a uniform 201 is coherent and matches Trakt's add-hiding muscle memory. Distinguishing `200`-on-already-hidden would add client-side contract complexity the spec doesn't call for. No code change; documented decision.)
- [ ] Verify module test coverage (`make test`).
