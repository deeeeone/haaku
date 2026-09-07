# Notifications Handler (`internal/handlers/notifications`)

## What it does

Implements the notification inbox and settings:

- `GET /v1/notifications` — paginated inbox, unread-first (`read ASC, created_at DESC`), with `unread_count` in the envelope.
- `POST /v1/notifications/read` — mark specific IDs read, or **all** read when the body is empty (or `ids` absent); a malformed non-empty body is a 400. Owner-scoped via `user_id`.
- `GET /v1/notifications/settings` — read the user's notification preferences (defaults all true; parsed from `user.settings` JSON under `notifications`).
- `CreateNotification` — exported helper for other handlers to create notifications inline (follow, reply, list invite).

All routes auth-required via the shared `auth.AuthRequired()` middleware. Registers OpenAPI metadata for the three endpoints.

## Completion

| Piece | Status |
|---|---|
| OpenAPI registration (3 routes) | Done |
| Paginated inbox with unread count | Done |
| Mark-read (specific + all) | Done |
| Settings read + defaults | Done |
| Exported `CreateNotification` helper | Done |

## Oversights

- **`requireAuth` hand-rolled (RESOLVED)** — routes now run shared `auth.AuthRequired()` (Batch 1).
- **`handleMarkRead` swallows the body-parse error (RESOLVED)** — a malformed non-empty JSON body now returns 400 `invalid_request`; only an empty/whitespace body means "mark ALL as read". A client typo no longer wipes the whole inbox.
- **No "read" settings write endpoint** — only `GET /settings`; no way to update notification preferences. Spec likely requires PUT/POST.
- **Settings stored as raw JSON string on the user row** — parsing is lenient (bad JSON → defaults), and unknown keys are ignored; no schema/versioning for settings.
- **Unread-first ordering isn't stable across pages** — `read ASC, created_at DESC` means reading one notification between page fetches shifts results; acceptable but worth noting (cursor pagination would be more robust).
- **`CreateNotification` not wrapped in cache-tx/outbox** — writes directly; called inline from other handlers, so it bypasses the transaction/invalidation conventions.
- **No settings-driven filtering at creation time** — `CreateNotification` doesn't check the recipient's `follows/replies/list_invite` preferences; notifications are created regardless.
- **Local error + pagination helpers** — `writeError`/`queryInt`/`clampLimit` duplicated.
- **No type/actor validation** — arbitrary `Type` strings accepted by `CreateNotification`.

## Todos

- [x] Return 400 on malformed JSON in `handleMarkRead` instead of marking everything read. (Batch 6)
- [x] Add a notification-settings update endpoint (spec check). (Batch 12 — DECISION: NOT in the spec → no endpoint added; documented as future.) `social-api.md`'s Notifications table lists exactly three routes — `GET /notifications`, `POST /notifications/read`, and `GET /notifications/settings` — and there is **no** settings-write route (no PUT/POST `/notifications/settings`). Per the AGENTS rule "invent no endpoints not in the specs," no update endpoint is added here. Updating preferences is recorded as future work (would write the `users.settings` JSON under `notifications`); for now settings are read-only via `GET /notifications/settings` and the write path remains unimplemented by design.)
- [x] Enforce notification preferences in `CreateNotification` (respect follows/replies/list_invite). (Batch 12 — implemented. `CreateNotification` now reads the recipient's settings (`users.settings` → `parseNotifSettings`) and skips creation when the toggle for the notification's type is disabled — follow→`follows`, reply→`replies`, list_invite→`list_invite`, import_complete→`import_complete`; types with no toggle (e.g. mentions) are always allowed. The underlying social action still happens; only the inbox entry is suppressed. Best-effort: a settings-read failure defaults to enabled so notifications are never silently lost. Exported `NotifType*` constants added for the spec's inbox categories.)
- [x] Move `CreateNotification` into the outbox/cache-tx flow. (Batch 7 — now takes the caller's `cacheTx` and bumps the recipient's user version; inserts must run inside the caller's `cache.WithCacheTransaction`)
- [x] Replace local error/pagination helpers with `internal/response` (Batch 0).
- [x] Swap `requireAuth` for shared `auth.AuthRequired` (Batch 1).
- [x] Consider cursor pagination for the inbox. (Batch 8 — decision: kept offset `page`/`limit` pagination and added §7 `X-Pagination-*` headers + `Link: rel="next"` with a stable `read ASC, created_at DESC, id DESC` order. Cursor was considered and rejected: the inbox already had an offset-based envelope contract and page/limit is what every other spec-compliant module (webhooks/sync/social) uses, so staying on offset keeps the API uniform. `unread_count`/`total_count` stay in the envelope; page determinism comes from the stable order + headers.)
- [ ] Verify module test coverage (`make test`).
