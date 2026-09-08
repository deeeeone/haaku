# Syncs Handler (`internal/handlers/syncs`)

## What it does

Batch import **sessions** (the umbrella for large history/ratings syncs — distinct from `sync`'s per-item endpoints):

- `POST /v1/syncs` — create a `SyncSession` (`kind` = history|ratings, `status: pending`, `total_items` from body). **Async processing is a stub** ("TODO: enqueue async processing").
- `GET /v1/syncs/:id` — session progress (owner-scoped `WHERE id = ? AND user_id = ?`).
- `GET /v1/syncs/:id/paused` — **fails closed with 501** `not_implemented` (unmatched/paused items require the sync engine, which isn't landed).
- `GET /v1/syncs/:id/skipped` — **fails closed with 501** `not_implemented` (skipped items require the sync engine, which isn't landed).

Uses the shared `auth.AuthRequired()` middleware on all four routes (one of the few modules doing this correctly). `:id<int>` param constraint. OpenAPI registered.

## Completion

| Piece | Status |
|---|---|
| OpenAPI registration (4 routes) | Done |
| Shared `auth.AuthRequired` on all routes | Done |
| Session create (DB row only) | Done |
| Session progress read | Done |
| Paused / skipped reads | Fail closed (501 `not_implemented` until the sync engine lands) |

## Oversights

- **Async engine is a stub (Batch 13)** — sessions are created `pending` and never processed; `/paused` and `/skipped` now **fail closed with 501 `not_implemented`** because they are produced by the sync engine (Phase 3+). Session create and progress reads remain real.
- **`Items []any` is stored nowhere** — the request body is only counted (`len(req.Items)`); the actual items are discarded and not persisted to the session (no items table). A resumed sync can never recover what was submitted.
- **`c.Locals("user_id").(string)` unchecked** in all three handlers — safe today because `auth.AuthRequired` runs first, but the pattern is fragile (see `collab`/`social`).
- **`c.ParamsInt("id")` errors ignored** — `syncID, _ := c.ParamsInt("id")`; the `:id<int>` route constraint should prevent non-numeric, but a 0/absent id yields a generic 404 instead of a 400.
- **Session not-found maps all DB errors to 404** — a DB outage reads as "sync session not found".
- **`/paused` no longer conflates "errors" with "unmatched items" (Batch 13)** — it previously dumped `session.Errors` (which may contain real processing errors, not paused/unmatched content); it now fails closed with 501 so no ambiguous/fabricated data is served until the engine distinguishes them.
- **No cache/outbox** — session reads are direct DB; no version-keyed caching. (Likely fine for now given low traffic, but inconsistent with the sync module.)
- **`buildSyncResponse` exposes `user_id` in the response** (its `.000Z` literals were unified to the shared `response.FormatTime` helper in Batch 3).
- **`Errors any` in the JSON response** — untyped; `buildSyncResponse` still forwards `session.Errors` (which stays empty until the engine writes it).

## Todos

- [ ] Implement (or explicitly scope) the async sync engine and persist submitted items.
- [x] Decide what `/paused` and `/skipped` return. (Batch 13 — DECISION: fail closed with 501 `not_implemented` until the sync engine lands; the previous raw-`Errors`/hardcoded-empty responses were placeholders and no longer served.)
- [ ] Once the engine exists, return real paused/skipped data from `/paused` and `/skipped` with pagination.
- [x] Replace unchecked `user_id` assert with a safe helper (`requireUserID`, 401 on missing).
- [x] Validate `:id<int>` parse and return 400 on failure (`parseSyncID`).
- [x] Separate "not found" from other DB errors — `First` failures that aren't `gorm.ErrRecordNotFound` now return 500 `internal` instead of 404.
- [x] Verify module test coverage (`make test`). (Batch 14 — `internal/handlers/syncs/syncs_test.go` covers `buildSyncResponse` and `parseSyncID` (valid / non-numeric / zero). Handler-level integration for the DB-backed sync-session CRUD awaits a DB-driver approval.)
