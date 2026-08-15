# Notifications Handler (`internal/handlers/notifications`)

## What it does

Implements the notification inbox and settings:

- `GET /v1/notifications` — paginated inbox, unread-first (`read ASC, created_at DESC`), with `unread_count` in the envelope.
- `POST /v1/notifications/read` — mark specific IDs read, or **all** read when the body is empty/invalid (or `ids` absent). Owner-scoped via `user_id`.
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
- **`handleMarkRead` swallows the body-parse error** (`_ = err`) — a malformed JSON body silently becomes "mark ALL as read". A client typo can wipe the read state of the whole inbox. Should 400 on malformed JSON.
- **No "read" settings write endpoint** — only `GET /settings`; no way to update notification preferences. Spec likely requires PUT/POST.
- **Settings stored as raw JSON string on the user row** — parsing is lenient (bad JSON → defaults), and unknown keys are ignored; no schema/versioning for settings.
- **Unread-first ordering isn't stable across pages** — `read ASC, created_at DESC` means reading one notification between page fetches shifts results; acceptable but worth noting (cursor pagination would be more robust).
- **`CreateNotification` not wrapped in cache-tx/outbox** — writes directly; called inline from other handlers, so it bypasses the transaction/invalidation conventions.
- **No settings-driven filtering at creation time** — `CreateNotification` doesn't check the recipient's `follows/replies/list_invite` preferences; notifications are created regardless.
- **Local error + pagination helpers** — `writeError`/`queryInt`/`clampLimit` duplicated.
- **No type/actor validation** — arbitrary `Type` strings accepted by `CreateNotification`.

## Todos

- [ ] Return 400 on malformed JSON in `handleMarkRead` instead of marking everything read.
- [ ] Add a notification-settings update endpoint (spec check).
- [ ] Enforce notification preferences in `CreateNotification` (respect follows/replies/list_invite).
- [ ] Move `CreateNotification` into the outbox/cache-tx flow.
- [x] Replace local error/pagination helpers with `internal/response` (Batch 0).
- [x] Swap `requireAuth` for shared `auth.AuthRequired` (Batch 1).
- [ ] Consider cursor pagination for the inbox.
- [ ] Verify module test coverage (`make test`).
