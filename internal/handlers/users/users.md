# Users Handler (`internal/handlers/users`)

## What it does

Account/profile management:

- `GET /v1/users/me` — full profile (incl. admin/moderator flags, plan, status).
- `PUT /v1/users/me` — partial profile update (name, bio, location, timezone, language, is_private).
- `GET /v1/users/:id` — public profile wrapped in the shared `auth.PrivacyFilter("id")` middleware; stats/activity are not fabricated (followers/following are real; watched/lists/activity omitted until the stats engine lands).
- `GET|PUT /v1/users/me/settings` — settings as JSON on the user row (privacy + notification prefs, defaulted/backfilled).
- ~~`POST /v1/users/me/avatar`~~ — **removed (2026-08)**. Avatars are Clerk-managed (mirrored via webhook/lazy sync); there is no upload route, so a placeholder `avatar_url` can never be persisted.
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
| Avatar upload | N/A — endpoint removed (Clerk-managed identity); no placeholder `avatar_url` ever persisted |
| Account deletion | Partial (no async cleanup) |
| Block/unblock | Done |

## Oversights

- **`uploadToS3` was a stub (RESOLVED — superseded by endpoint removal)** — it used to validate then discard the file and return a placeholder URL (`fmt.Sprintf("%s/%s/%s", endpoint, bucket, key)`, users.go:607-616), and the handler persisted it to `avatar_url`. That stub is gone: the handler was first made to fail closed with **501 `not_implemented`** (no placeholder persisted), and the `/users/me/avatar` endpoint was subsequently **removed entirely (2026-08)** in favor of Clerk-managed identity — `avatar_url` is only mirrored from the Clerk account. No placeholder `avatar_url` can ever be persisted.
- **Block/unblock not wrapped in cache-tx** — plain `db` writes + outbox published outside a transaction (users.go:452-466); follows deletion is 2 separate deletes (no transaction), matching `social`'s pattern rather than the checkin/scrobble standard. Raw string event names (`"user.blocked"`/`"user.unblocked"`).
- **No outbox/notification on delete-account** — `pending_deletion` status set, no `account.deletion_scheduled` event, no cache invalidation for the user's cached profiles.
- **`handleUpdateMe` ignores the post-update `db.First` error (RESOLVED)** — a failed re-fetch now logs and returns 500 `internal` instead of returning the pre-update row with a 200.
- **`parseSettings` backfill is asymmetric** — missing `privacy` fields are backfilled with defaults, but missing `notifications` fields are not (a partial settings JSON yields false notification prefs). Also the `notifications` module re-parses these independently — duplicated parsing logic.
- **Public profile stats are not fabricated (Batch 13)** — `Watched`/`Lists` were hardcoded 0 and `RecentActivity` empty. These fields are spec'd to be filled by the stats engine (not landed), so they are no longer presented as if real: `watched`/`lists` are now `omitempty` (a 0 is not emitted as a real count) and `recent_activity` stays omitted; only `followers`/`following` are returned as real DB-computed counts. Followers/following still don't filter `approved_at`. Real stats wiring remains an open todo.
- **`/users/me` routes registered as `me.Get("/")`** — Fiber group prefix + `"/"` may only match `/users/me/` (trailing slash) and not `/users/me`; verify routing (the later `/users/:id` registration could swallow `/users/me`).
- **Local `requireAuth` (RESOLVED)** — `/users/me` and block routes now run shared `auth.AuthRequired()` (Batch 1); the local `writeError` was replaced by `internal/response` (Batch 0).
- **Profile writes not cache-invalidated** — avatar URL and profile updates change cached public profiles without bumping the user version counter.
- **No validation on timezone/language/bio length** — arbitrary strings accepted (spec may cap lengths).

## Todos

- [x] Avatar handling is honest — no placeholder `avatar_url` is ever persisted. (Batch 13 — VERIFIED: the `/users/me/avatar` route was removed entirely in favor of Clerk-managed identity; `avatar_url` is only mirrored from the Clerk account via webhook/lazy sync, and `UpdateProfileRequest` deliberately excludes it. No upload handler or S3 stub remains.)
- [ ] Implement real S3/MinIO upload once the dependency is approved (only if self-uploaded avatars return; currently Clerk-managed).
- [x] Wrap block/unblock/delete-account in `cache.WithCacheTransaction` with version invalidation + in-tx outbox; use typed event constants. (Batch 7 — block emits typed `follow.removed`; raw `user.blocked`/`user.unblocked` strings removed)
- [x] Emit account-deletion outbox event and invalidate cached profiles. (Batch 7 — typed `user.deleted`)
- [x] Handle the post-update re-fetch error in `handleUpdateMe`. (Batch 6)
- [ ] Backfill notification settings symmetrically in `parseSettings`.
- [x] Verify `/users/me` vs `/users/:id` route precedence (trailing-slash issue) — static route wins; added TestUsersMeRoutePrecedence regression test.
- [x] Public profile must not fabricate aggregate stats. (Batch 13 — DECISION: `watched`/`lists` are spec'd to be filled by the stats engine, which isn't landed, so hardcoded 0s are no longer emitted (fields are `omitempty`); `recent_activity` stays omitted; `followers`/`following` remain real DB counts. No counts are fabricated.)
- [ ] Wire real stats + recent activity into the public profile (stats engine).
- [x] Use shared `internal/response` error helpers (Batch 0).
- [x] Swap local `requireAuth` for shared `auth.AuthRequired` (Batch 1).
- [x] Verify module test coverage (`make test`). (Batch 14 — `internal/handlers/users/users_test.go` covers `defaultSettings`, `parseSettings` (defaults/overrides + privacy-only backfill), `buildProfileResponse`, and `buildPublicProfileResponse`. Handler-level integration for the DB+Clerk profile/settings/block paths awaits a DB-driver approval.)
