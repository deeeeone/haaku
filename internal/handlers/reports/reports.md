# Reports Handler (`internal/handlers/reports`)

## What it does

Content moderation reporting, four POST endpoints (one per media type):

- `POST /v1/{movies|shows|seasons|episodes}/:id/report` — create a `ContentReport` (reporter_id, media_type, media_id, reason, description). All funnel through `handleReportContent`.

Validation: `id` must parse; `reason` allowlisted (`spam`, `spoiler`, `offensive`, `incorrect`, `duplicate`, `other`); duplicate report by same user+media → 400. Auth via the shared `auth.AuthRequired()` middleware (registered on routes). Returns 201 with the created report. OpenAPI registered.

## Completion

| Piece | Status |
|---|---|
| OpenAPI registration (4 routes) | Done |
| Per-type handlers + shared impl | Done |
| Reason allowlist validation | Done |
| Duplicate-report detection | Done |
| 404/400/401 status codes per OpenAPI | Partial (see below) |

## Oversights

- **No FK/media existence check (RESOLVED)** — `handleReportContent` now verifies the movie/show/season/episode row exists (`reportTargetExists`) before inserting, so the OpenAPI-advertised 404 is actually returned for missing targets.
- **No per-user rate limit** — nothing stops a user hammering report endpoints; charter anti-abuse (429) not applied here.
- **Check-then-insert race** — duplicate check (`First`) then `Create` is not atomic; two concurrent requests can both pass the check and create duplicates (no unique index on `(reporter_id, media_type, media_id)`? — verify schema).
- **Local `writeError`/`badRequest`** duplicated from `internal/httpapi`.
- **`toReportResponse` exposes `reporter_id`** — leaks the reporting user's ID in the response body (may be fine for moderation but unusual for a public report create).
- **`c.Locals("user_id").(string)` unchecked** in `handleReportContent` (line 120) — depends entirely on `requireAuth`; a future route using this helper without middleware panics (same pattern as `collab`).
- **Hand-rolled `requireAuth` (RESOLVED)** — routes now run shared `auth.AuthRequired()` (Batch 1).
- **No `extended`/description length cap** — `Description` is unbounded.
- **No outbox/cache invalidation** — moderation events not streamed anywhere.

## Todos

- [x] Verify target media exists before insert (return the advertised 404) or drop 404 from OpenAPI. (Batch 6)
- [x] Add per-user 429 rate limiting on report submission. (Batch 11)
- [ ] Add a unique constraint on `(reporter_id, media_type, media_id)` (schema change → planning DB doc first) or use upsert/on-conflict.
- [x] Use shared `internal/response` error helpers (Batch 0).
- [x] Swap `requireAuth` for shared `auth.AuthRequired` (Batch 1).
- [ ] Consider whether `reporter_id` should be in the public response.
- [x] Cap `description` length. (Batch 11)
- [x] Verify module test coverage (`make test`). (Batch 14 — `internal/handlers/reports/reports_test.go` covers the `validReportReasons` allowlist, `maxReportDescription` cap, and `toReportResponse`. Handler-level integration for the DB+Redis rate-limited submit path awaits a DB-driver approval.)
