# Users Handler (`internal/handlers/users`)

## What it does

Account/profile management:

- `GET /v1/users/me` — full profile (incl. admin/moderator flags, plan, status).
- `PUT /v1/users/me` — partial profile update (name, bio, location, timezone, language, is_private).
- `GET /v1/users/:id` — public profile wrapped in the shared `auth.PrivacyFilter("id")` middleware; stats/activity are stubs.
- `GET|PUT /v1/users/me/settings` — settings as JSON on the user row (privacy + notification prefs, defaulted/backfilled).
- `POST /v1/users/me/avatar` — multipart avatar upload (ext + content-type + 5MB validation).
- `DELETE /v1/users/me` — soft-delete (`status = pending_deletion`; async cleanup is a stub).
- `POST|DELETE /v1/users/:id/block` — block (removes follows both directions, outbox `user.blocked`) / unblock.

Uses shared `auth.PrivacyFilter` for the public route; shared `auth.AuthRequired()` elsewhere. OpenAPI registered.

## Completion

| Piece | Status |
|---|---|
| OpenAPI registration (9 routes) | Done |
| Me profile get/update | Done |
| Public profile + privacy filter | Done |
| Settings get/update | Done |
| Avatar upload (validation + S3) | Partial (fails closed — 501 until real S3 client) |
| Account deletion | Partial (no async cleanup) |
| Block/unblock | Done |

## Oversights

- **`uploadToS3` was a stub (RESOLVED — fail-closed)** — it validated then discarded the file and returned a placeholder URL (`fmt.Sprintf("%s/%s/%s", endpoint, bucket, key)`, users.go:607-616), and the handler persisted it to `avatar_url`, so users got a URL pointing at a file that was never stored. `uploadToS3` now returns an error and the handler returns **501 `{"error":{"code":"not_implemented","message":"..."}}`** without touching `avatar_url` (no placeholder URL is ever persisted). A real S3/MinIO client still needs an approved dependency.
- **Block/unblock not wrapped in cache-tx** — plain `db` writes + outbox published outside a transaction (users.go:452-466); follows deletion is 2 separate deletes (no transaction), matching `social`'s pattern rather than the checkin/scrobble standard. Raw string event names (`"user.blocked"`/`"user.unblocked"`).
- **No outbox/notification on delete-account** — `pending_deletion` status set, no `account.deletion_scheduled` event, no cache invalidation for the user's cached profiles.
- **`handleUpdateMe` ignores the post-update `db.First` error** — a failed re-fetch returns the pre-update row with a 200.
- **`parseSettings` backfill is asymmetric** — missing `privacy` fields are backfilled with defaults, but missing `notifications` fields are not (a partial settings JSON yields false notification prefs). Also the `notifications` module re-parses these independently — duplicated parsing logic.
- **Public profile stats are stubs** — `Watched`/`Lists` hardcoded 0, `RecentActivity` empty; `StatsSummary` never wired to the stats engine. Followers/following counts don't filter `approved_at`.
- **`/users/me` routes registered as `me.Get("/")`** — Fiber group prefix + `"/"` may only match `/users/me/` (trailing slash) and not `/users/me`; verify routing (the later `/users/:id` registration could swallow `/users/me`).
- **Local `requireAuth` (RESOLVED)** — `/users/me` and block routes now run shared `auth.AuthRequired()` (Batch 1); the local `writeError` was replaced by `internal/response` (Batch 0).
- **Profile writes not cache-invalidated** — avatar URL and profile updates change cached public profiles without bumping the user version counter.
- **No validation on timezone/language/bio length** — arbitrary strings accepted (spec may cap lengths).

## Todos

- [x] Gate avatar upload until S3 is implemented — endpoint now fails closed with 501 `not_implemented` (no fake URL persisted).
- [ ] Implement real S3/MinIO upload once the dependency is approved.
- [ ] Wrap block/unblock/delete-account in `cache.WithCacheTransaction` with version invalidation + in-tx outbox; use typed event constants.
- [ ] Emit account-deletion outbox event and invalidate cached profiles.
- [ ] Handle the post-update re-fetch error in `handleUpdateMe`.
- [ ] Backfill notification settings symmetrically in `parseSettings`.
- [x] Verify `/users/me` vs `/users/:id` route precedence (trailing-slash issue) — static route wins; added TestUsersMeRoutePrecedence regression test.
- [ ] Wire real stats + recent activity into the public profile.
- [x] Use shared `internal/response` error helpers (Batch 0).
- [x] Swap local `requireAuth` for shared `auth.AuthRequired` (Batch 1).
- [ ] Verify module test coverage (`make test`).
