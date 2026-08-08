# Syncs Handler (`internal/handlers/syncs`)

## What it does

Batch import **sessions** (the umbrella for large history/ratings syncs — distinct from `sync`'s per-item endpoints):

- `POST /v1/syncs` — create a `SyncSession` (`kind` = history|ratings, `status: pending`, `total_items` from body). **Async processing is a stub** ("TODO: enqueue async processing").
- `GET /v1/syncs/:id` — session progress (owner-scoped `WHERE id = ? AND user_id = ?`).
- `GET /v1/syncs/:id/paused` — unmatched/paused items (currently dumps `session.Errors`).
- `GET /v1/syncs/:id/skipped` — skipped items (hardcoded empty).

Uses the shared `auth.AuthRequired()` middleware on all four routes (one of the few modules doing this correctly). `:id<int>` param constraint. OpenAPI registered.

## Completion

| Piece | Status |
|---|---|
| OpenAPI registration (4 routes) | Done |
| Shared `auth.AuthRequired` on all routes | Done |
| Session create (DB row only) | Done |
| Session progress read | Done |
| Paused / skipped reads | Stub |

## Oversights

- **Async engine is a stub** — sessions are created `pending` and never processed; `/paused` and `/skipped` return raw `Errors`/empty. The module is effectively a placeholder until the sync engine lands (Phase 3+ per comments).
- **`Items []any` is stored nowhere** — the request body is only counted (`len(req.Items)`); the actual items are discarded and not persisted to the session (no items table). A resumed sync can never recover what was submitted.
- **`c.Locals("user_id").(string)` unchecked** in all three handlers — safe today because `auth.AuthRequired` runs first, but the pattern is fragile (see `collab`/`social`).
- **`c.ParamsInt("id")` errors ignored** — `syncID, _ := c.ParamsInt("id")`; the `:id<int>` route constraint should prevent non-numeric, but a 0/absent id yields a generic 404 instead of a 400.
- **Session not-found maps all DB errors to 404** — a DB outage reads as "sync session not found".
- **`/paused` conflates "errors" with "unmatched items"** — `session.Errors` may contain real processing errors, not paused/unmatched content; no distinction.
- **No cache/outbox** — session reads are direct DB; no version-keyed caching. (Likely fine for now given low traffic, but inconsistent with the sync module.)
- **`buildSyncResponse` uses `.000Z` timestamp literals** and exposes `user_id` in the response.
- **`Errors any` in the JSON response** — untyped; the paused response duplicates this via `session.Errors`.

## Todos

- [ ] Implement (or explicitly scope) the async sync engine and persist submitted items.
- [ ] Decide what `/paused` and `/skipped` return once the engine exists; add real pagination.
- [x] Replace unchecked `user_id` assert with a safe helper (`requireUserID`, 401 on missing).
- [x] Validate `:id<int>` parse and return 400 on failure (`parseSyncID`).
- [ ] Separate "not found" from other DB errors.
- [ ] Verify module test coverage (`make test`).
