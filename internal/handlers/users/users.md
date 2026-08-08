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

Uses shared `auth.PrivacyFilter` for the public route; local `requireAuth` elsewhere. OpenAPI registered.

## Completion

| Piece | Status |
|---|---|
| OpenAPI registration (9 routes) | Done |
| Me profile get/update | Done |
| Public profile + privacy filter | Done |
| Settings get/update | Done |
| Avatar upload (validation + S3) | Partial (S3 is a stub) |
| Account deletion | Partial (no async cleanup) |
| Block/unblock | Done |

## Oversights

- **`uploadToS3` is a stub** — validates then discards the file and returns a placeholder URL (`fmt.Sprintf("%s/%s/%s", endpoint, bucket, key)`, users.go:607-616). Files are never stored; `db.Model(...).Update("avatar_url", avatarURL)` then points at a URL that doesn't exist. TODO admits no real S3 client (dep not approved).
- **Block/unblock not wrapped in cache-tx** — plain `db` writes + outbox published outside a transaction (users.go:452-466); follows deletion is 2 separate deletes (no transaction), matching `social`'s pattern rather than the checkin/scrobble standard. Raw string event names (`"user.blocked"`/`"user.unblocked"`).
- **No outbox/notification on delete-account** — `pending_deletion` status set, no `account.deletion_scheduled` event, no cache invalidation for the user's cached profiles.
- **`handleUpdateMe` ignores the post-update `db.First` error** — a failed re-fetch returns the pre-update row with a 200.
- **`parseSettings` backfill is asymmetric** — missing `privacy` fields are backfilled with defaults, but missing `notifications` fields are not (a partial settings JSON yields false notification prefs). Also the `notifications` module re-parses these independently — duplicated parsing logic.
- **Public profile stats are stubs** — `Watched`/`Lists` hardcoded 0, `RecentActivity` empty; `StatsSummary` never wired to the stats engine. Followers/following counts don't filter `approved_at`.
- **`/users/me` routes registered as `me.Get("/")`** — Fiber group prefix + `"/"` may only match `/users/me/` (trailing slash) and not `/users/me`; verify routing (the later `/users/:id` registration could swallow `/users/me`).
- **Local `requireAuth`** duplicates `auth.AuthRequired`; local `writeError` duplicates `internal/httpapi`.
- **Profile writes not cache-invalidated** — avatar URL and profile updates change cached public profiles without bumping the user version counter.
- **No validation on timezone/language/bio length** — arbitrary strings accepted (spec may cap lengths).

## Todos

- [ ] Implement real S3/MinIO upload (or gate the endpoint until the dep is approved — currently returns 200 with a fake URL).
- [ ] Wrap block/unblock/delete-account in `cache.WithCacheTransaction` with version invalidation + in-tx outbox; use typed event constants.
- [ ] Emit account-deletion outbox event and invalidate cached profiles.
- [ ] Handle the post-update re-fetch error in `handleUpdateMe`.
- [ ] Backfill notification settings symmetrically in `parseSettings`.
- [ ] Verify `/users/me` vs `/users/:id` route precedence (trailing-slash issue).
- [ ] Wire real stats + recent activity into the public profile.
- [ ] Use shared `auth.AuthRequired` / `internal/httpapi` helpers.
- [ ] Verify module test coverage (`make test`).
