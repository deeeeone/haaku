# Reports Handler (`internal/handlers/reports`)

## What it does

Content moderation reporting and the operator moderation queue that acts on it.

**Submission** — four POST endpoints (one per media type):

- `POST /v1/{movies|shows|seasons|episodes}/:id/report` — create a `ContentReport` (reporter_id, media_type, media_id, reason, description). All funnel through `handleReportContent`.

Validation: `id` must parse; `reason` allowlisted (`spam`, `spoiler`, `offensive`, `incorrect`, `duplicate`, `other`); duplicate report by same user+media → 400; description capped at 1000 chars; per-user 429 rate limit. Auth via the shared `auth.AuthRequired()` middleware. Returns 201 with the created report (200 if a dismissed report was reopened — see below). OpenAPI registered.

**Moderation queue (issue #10 §2)** — the operator surface over the same rows:

- `GET /v1/reports` — list reports; filters `?status=` (`open`|`resolved`|`dismissed`) and `?target_type=` (`movie`|`show`|`season`|`episode`); paginated (§7 headers, default 25/page). Invalid filter → 400. Newest first.
- `POST /v1/reports/:id/resolve` — move an open report to `resolved` (action taken). Optional JSON body `{"note": "..."}` (≤1000 chars).
- `POST /v1/reports/:id/dismiss` — move an open report to `dismissed` (no action needed). Same body.

Both action endpoints record the acting operator (`resolved_by`), the note (`resolution_note`) and the time (`resolved_at`) — the audit trail. A second action on an already-terminal report is **409**, not a silent no-op. Missing report → 404. Bad body / bad note → 400. Auth: `auth.AuthRequired()` then `auth.RequireModerator()` → 401 unauthenticated, 403 non-moderator.

## Authorization model (the decision — needs the owner's review)

The codebase has no separate "admin role" system: only the `users.is_admin` / `users.is_moderator` boolean columns, which the comment-delete moderator override (`comments/comments.go`) and the `PrivacyFilter` admin bypass already trust — but **nothing in the app writes those columns**. `ADMIN_USER_IDS` / `MOD_USER_IDS` were parsed by `internal/config` and never consulted.

`auth.RequireModerator()` (new, `internal/auth/moderation.go`) therefore authorizes on **either** mechanism the codebase can honestly support:

1. the env allowlists `ADMIN_USER_IDS` / `MOD_USER_IDS` (recorded at startup by `auth.InitModerationAllowlists`, wired in `httpapi.NewServer`), so an operator can grant moderation on a self-hosted box without database surgery; **or**
2. a `users` row with `is_moderator` or `is_admin` true.

The two env vars collapse into one moderator set: the queue needs a single "may action reports" gate, and an admin is a moderator by definition. A DB failure fails closed (500); an authenticated principal with no user row is denied (403). **This is a deliberate choice and is flagged for owner review in the PR** — the alternative (env allowlist only, or DB column only) is a one-line change here.

## Terminal state and re-reporting

Migration `0029` created `content_reports` with no lifecycle columns and a unique index on `(reporter_id, media_type, media_id)`; a report could be filed but never actioned. The lifecycle columns (`status`, `resolution_note`, `resolved_by`, `resolved_at`, `updated_at`) are added by the append-only migration `20261106212954_content_report_moderation.sql` (collection `migrations/sql/`) — 0029 is untouched. `status` defaults to `open`, so every pre-existing row is open.

Re-reporting is handled at the application layer because the 0029 unique index is unconditional and sqlite (the handler-test driver) has no partial indexes:

- existing report **open** or **resolved** → 400 (`you have already reported this content`);
- existing report **dismissed** → the single row is **reopened** (status back to `open`, reason/description refreshed, resolution fields cleared), returning **200**. A dismissal means "no action needed", not "this user may never flag the content again". No new row, so no unique-index change is needed.
- a concurrent identical report can still trip the 0029 index between the check and the insert; that race is now caught and answered 400 (not a misleading 500).

No content state (hidden/removed) is added: the schema does not model a reported-content state, and takedown is out of scope (see below).

## Completion

| Piece | Status |
|---|---|
| OpenAPI registration (4 submit routes) | Done |
| OpenAPI registration (list + resolve + dismiss) | Done |
| Per-type handlers + shared impl | Done |
| Reason allowlist validation | Done |
| Duplicate-report detection | Done |
| Moderation queue: list + filters + pagination | Done |
| Moderation actions: resolve/dismiss with audit record | Done |
| Terminal-state schema (append-only migration) | Done |
| Reopen-on-re-report after dismissal | Done |
| Authorization gate (`auth.RequireModerator`) | Done |
| 404/400/401/403 status codes per OpenAPI | Done |

## Oversights (residue of #10 §2 — not done here)

- **No content removal/takedown / soft-hide** — resolving a report records a decision; it does not hide, delete, or mark the reported content, and no author notification is sent. The schema models no reported-content state, so this is a content-table change and is deliberately out of scope.
- **No retention window / queue pruning** — `content_reports` grows unbounded; §6 owns retention.
- **No admin/mod UI** — the frontend has no moderation surface; these are API endpoints only.
- **`reporter_id` still returned by the submit response** — a reporter seeing their own id back is harmless, but the audit note ("Stop leaking reporter_id in the public response") is unaddressed; the queue (moderator-only) needs `reporter_id` for the audit trail.
- **Moderator comment-delete reply-count cascade** — still stale on delete; a separate item in #10 §2.
- **`toReportResponse` includes `reporter_id`** on the moderator list — intended (audit), but worth a decision.
- **Hand-rolled `requireAuth` (RESOLVED)** — routes run shared `auth.AuthRequired()` (Batch 1).
- **`c.Locals("user_id").(string)` unchecked** in the handlers — depends on the auth middleware; a route registered without it panics.

## Todos

- [x] Verify target media exists before insert (return the advertised 404) — Batch 6.
- [x] Add per-user 429 rate limiting on report submission — Batch 11.
- [x] Unique constraint on `(reporter_id, media_type, media_id)` — present in 0029; the create path now tolerates the race (400, not 500).
- [x] Use shared `internal/response` error helpers — Batch 0.
- [x] Swap `requireAuth` for shared `auth.AuthRequired` — Batch 1.
- [x] Cap `description` length — Batch 11.
- [x] Moderation queue + resolve/dismiss + audit record (issue #10 §2).
- [x] Terminal-state migration (append-only).
- [x] Handler tests against in-memory sqlite (200/400/401/403/404) + auth middleware tests.
- [ ] Takedown/soft-hide of reported content with author notification (#10 §2).
- [ ] Decide whether `reporter_id` belongs in the submit response.
