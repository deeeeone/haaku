# Syncs Handler (`internal/handlers/syncs`)

## What it does

Batch import **sessions** (the umbrella for large history/ratings syncs — distinct from `sync`'s per-item endpoints):

- `POST /v1/syncs` — create a `SyncSession` (`kind` = history|ratings, `status: pending`, `total_items` from body) **and persist the submitted items** as `sync_session_items` rows, in one transaction with the session. Each item must be a JSON object; a non-object entry is rejected `400 invalid_request` rather than stored as an item the engine could never resolve.
- `GET /v1/syncs/:id` — session progress (owner-scoped `WHERE id = ? AND user_id = ?`).
- `GET /v1/syncs/:id/paused` — the items the engine could not add: `not_found` (id did not resolve) and `error` (a poisoned item). Paginated.
- `GET /v1/syncs/:id/skipped` — the items the engine deliberately did not write because the collection already had them (`duplicate`). Paginated.

Uses the shared `auth.AuthRequired()` middleware on all four routes (one of the few modules doing this correctly). `:id<int>` param constraint. OpenAPI registered.

The request path never resolves or writes a single item: resolution and collection writes are the async engine's job. See `internal/syncengine/processor.go` — the consumer `POST /v1/syncs` has always pointed at.

Related, in the sibling `sync` package: `GET /v1/sync/last_activities` (issue #6, first slice) shipped — the Trakt "what changed since" delta a client polls to decide *what* to sync. See `internal/handlers/sync/sync.md`.

## The engine

`internal/syncengine` (issue #6) claims a `pending` session, walks its persisted items, resolves each against our media data, writes the target collection, records a per-item outcome, and drives the session to a terminal state. It is its own package with **no HTTP and no scheduling**: a worker (owned by the worker-topology lane) calls `syncengine.ProcessPendingSessions(db, limit)`. Nothing starts a goroutine or a ticker, so the API request path can never run a session.

Guarantees, and where each lives:

- **idempotent** — only `state='pending'` items are worked, and a session is claimed by a compare-and-swap `pending -> processing`; a re-run claims nothing. Collection writes are additionally `ON CONFLICT DO NOTHING` / upsert.
- **concurrency-safe** — the claim runs under a PostgreSQL advisory lock (`database.WithAdvisoryLockStr`).
- **poison-tolerant** — a bad item is recorded as `outcome='error'` and the session continues; a panic in one item is recovered and recorded.

Terminal per-item outcomes (a closed set): `added`, `not_found`, `duplicate`, `error`. `/paused` serves `not_found` + `error`; `/skipped` serves `duplicate`.

`sync_sessions.errors` is now the typed `database.SyncErrors` object (issue #6) the engine writes when a session finishes: `counts` (one tally per outcome) and `items` (each item that did not land — `not_found` + `error` — with a machine-readable `code`, a message, and the item's ids). It replaced an untyped map that nothing ever wrote.

## Completion

| Piece | Status |
|---|---|
| OpenAPI registration (4 routes) | Done |
| Shared `auth.AuthRequired` on all routes | Done |
| Session create + item persistence | Done (session + items in one transaction) |
| Session progress read | Done |
| Paused / skipped reads | Done — real paginated engine output |
| Async engine (`internal/syncengine`) | Done — worker-callable; invoked by the worker lane |
| `/v1/sync/last_activities` (delta endpoint, `sync` pkg) | Done (issue #6, first slice) |

## Oversights

- **Nothing calls the engine in-process** — `ProcessPendingSessions` has no caller in this repo yet. The worker-topology lane owns `cmd/` and the worker loop; this PR supplies the seam (`ProcessPendingSessions(db, limit)`) and does not fight it for those files.
- **`Errors` is owner-visible and only error/not_found detail** — the per-item list is capped at 100 in `sync_sessions.errors`; the full per-item record always stays in `sync_session_items`.
- **`c.Locals("user_id").(string)` unchecked** — safe today because `auth.AuthRequired` runs first, but the pattern is fragile (see `collab`/`social`).
- **Session not-found maps all DB errors to 404** in `handleGetSync` — a DB outage reads as "sync session not found". (`serveSessionItems` distinguishes them: non-`ErrRecordNotFound` returns 500.)
- **No cache/outbox** — session reads are direct DB; no version-keyed caching. (Likely fine for now given low traffic, but inconsistent with the sync module.)
- **`buildSyncResponse` exposes `user_id` in the response.**
- **Episode resolution walks show -> season -> episode** per item (`resolveEpisode`), like the synchronous `/v1/sync` endpoints. A batch-level season/episode prefetch would cut queries if episode-heavy imports prove hot.

## Todos

- [x] Implement the async sync engine and persist submitted items. (Engine in `internal/syncengine`; items in `sync_session_items`.)
- [x] `GET /v1/sync/last_activities` — landed as a standalone delta endpoint in `internal/handlers/sync` (issue #6, first slice).
- [x] Decide what `/paused` and `/skipped` return — real engine output, paginated (no more 501).
- [x] Persist `session.Errors` with a typed shape.
- [x] Verify module test coverage — `syncs_test.go` covers create -> persist, item validation, paused/skipped reads and owner scoping; `syncengine/processor_test.go` covers create -> process -> read-back including `not_found`.
- [ ] Wire `ProcessPendingSessions` into the worker loop (worker-topology lane).
