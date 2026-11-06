# Users Handler (`internal/handlers/users`)

## What it does

Account/profile management:

- `GET /v1/users/me` — full profile (incl. admin/moderator flags, plan, status).
- `PUT /v1/users/me` — partial profile update (name, bio, location, timezone, language, is_private).
- `GET /v1/users/:id` — public profile wrapped in the shared `auth.PrivacyFilter("id")` middleware; stats/activity are not fabricated (followers/following are real; watched/lists/activity omitted until the stats engine lands).
- **Public-profile read surface** (`profile_reads.go`, issue #4 Users section) — read-only views of another user's data, every `:id` route behind `auth.PrivacyFilter("id")` and the same deleted/pending-deletion gate `handleGetUser` uses (`profileTarget`):
  - `GET /v1/users/:id/history`, `/history/{movies|shows|seasons|episodes|all}`, `/history/{type}/{id}` (`?id_type=` resolves an external id).
  - `GET /v1/users/:id/ratings` and `/ratings/{type}`.
  - `GET /v1/users/:id/watchlist` and `/watchlist/{type}` (`?sort_by=rank|listed|added|title`, `?sort_how=asc|desc`).
  - `GET /v1/users/:id/collection` and `/collection/{type}`.
  - `GET /v1/users/:id/watching` — real predicate (see "Watching" below).
  - `GET /v1/users/:id/comments/{all|reviews|shouts}/{all|movies|shows|seasons|episodes|lists}`.
  - `GET /v1/users/:id/notes/{type}`.
  - `GET /v1/users/:id/likes/comments`, `/likes/lists`; plus the caller's own `GET /v1/users/me/likes/comments`, `/me/likes/lists`.
  - Response shapes are the ones the caller's own `/v1/sync/...`, `/v1/comments/...` and `/v1/notes/...` routes return (exported aliases in those packages: `sync.BuildHistoryResponse`, `comments.ToCommentResponse`, `notes.ToNoteResponse`) — list endpoints use the `{items,page,limit,row_count}` envelope, comments/notes use their own `{data,page,limit,total_count}`. Empty collections are `[]`, never `null`.
- `GET|PUT /v1/users/me/settings` — settings as JSON on the user row (privacy + notification prefs, defaulted/backfilled).
- `GET /v1/users/me/export` — owner-scoped, **streamed** JSON archive of everything the caller owns (`export.go`, issue #10 §1). See "Data export" below.
- ~~`POST /v1/users/me/avatar`~~ — **removed (2026-08)**. Avatars are Clerk-managed (mirrored via webhook/lazy sync); there is no upload route, so a placeholder `avatar_url` can never be persisted.
- `DELETE /v1/users/me` — soft-delete (`status = pending_deletion`; async cleanup is a stub).
- `POST|DELETE /v1/users/:id/block` — block (removes follows both directions, outbox `user.blocked`) / unblock.

Uses shared `auth.PrivacyFilter` for the public route; shared `auth.AuthRequired()` elsewhere. OpenAPI registered.

## Completion

| Piece | Status |
|---|---|
| OpenAPI registration (10 routes) | Done |
| Me profile get/update | Done |
| Public profile + privacy filter | Done |
| **Public-profile read surface** (history/ratings/watchlist/collection/watching/comments/notes/likes, issue #4) | Done (read-only; stats-engine-dependent pieces out of scope) |
| Settings get/update | Done |
| **Data export** (`GET /users/me/export`, streamed, versioned) | Done (issue #10 §1) |
| Avatar upload | N/A — endpoint removed (Clerk-managed identity); no placeholder `avatar_url` ever persisted |
| Account deletion | Partial (no async cleanup) |
| Block/unblock | Done |

## Data export (`GET /v1/users/me/export`)

Owner-scoped archive of everything the caller owns. Registered in OpenAPI with
`200` / `401` / `503`. The response is **streamed** — a `json.Encoder` writes into
the body stream (`SetBodyStreamWriter`) and each table is walked with a
`database/sql` cursor (`streamModel`) so no section — let alone the whole archive
— is held in memory. `lists` is paged by keyset and each list's `items` array is
streamed nested inside it. `DB unavailable → 503`, `missing auth → 401`,
`user not found → 404` are all emitted as the standard envelope *before* the
stream starts (a status code cannot change once the body begins).

Archive shape (`ExportFormatVersion = 1`), documented in `export.go`:

```
{ version, exported_at, user, settings,
  history[], ratings[], watchlist[], collection[], favorites[],
  lists[{ ...list, items[] }], comments[], notes[], hidden[],
  follows{ following[], followers[] }, blocks[],
  checkins[], playback[], webhook_endpoints[] }
```

Empty sections serialise as `[]` (or `{}` for `follows`/`settings`), never `null`.

**Secrets are never exported.** The webhook section exposes the endpoint's
existence/URL/events — not `secret_hash`; integration tokens, API keys and OAuth
credentials are not read at all. A test asserts the seeded secret *values* are
absent from the serialised bytes.

Intended to round-trip with the importer in issue #7 (same format family). A
Trakt-compatible export shape is a **follow-up**, not a second format today.

## Public-profile read surface — privacy model and what it does NOT gate

Every `/v1/users/:id/...` route added for issue #4 runs `auth.PrivacyFilter("id")`
first, then `profileTarget`. The combined behaviour is:

| Request | Result |
|---|---|
| unknown id | 404 (PrivacyFilter) |
| block in either direction (caller ↔ target) | 404 — existence is not leaked |
| `status = deleted` / `pending_deletion` | 404 (`profileTarget`; PrivacyFilter does not look at status) |
| private profile, caller is a stranger | 403 |
| private profile, caller is self / admin / a follower | 200 |
| public profile | 200 for anyone, including anonymous |

### What the filter does NOT gate (deliberate, must not be misread as covered)

- **Per-section privacy settings.** `users.settings.privacy.{profile,history,ratings,activity}`
  ("public"/"friends"/"private") are stored and returned by `/users/me/settings`, but
  **nothing enforces them**. History/ratings/activity visibility is currently all-or-nothing
  via the single `is_private` flag. A user who sets `privacy.history = private` while
  `is_private = false` still gets a public `/users/:id/history`.
- **User-authored content and authorship.** For a public profile, comment bodies
  (`comment.body`), note bodies (`user_notes.note`) and the `user_id` on every comment are
  returned. The comments route filters by author (`user_id = target`), so third-party replies
  are not surfaced through it — but the target's own comment bodies and the fact that they
  authored them are public, and list comments (`/comments/all/lists`) are visible regardless
  of the list's own privacy. The filter never redacts comment text.
- **Liked lists.** `/users/:id/likes/lists` omits a liked list the caller may not see
  (private and not owned by the caller); `row_count` still counts the like rows, so an
  omission shows up as `items < row_count`.
- **Pre-existing routes that bypass PrivacyFilter.** `/v1/users/:id/lists` (lists package) and
  `/v1/users/:id/stats` (stats package) are registered with `auth.AuthRequired()` only — an
  authenticated stranger can read a private user's lists. That is a pre-existing gap, not
  introduced here (flagged, not fixed, to keep this PR scoped to new routes).
- **`user_playback` / `user_checkins`.** Not exposed at all; the only "activity" read is
  `watching` below.

### Watching (`GET /v1/users/:id/watching`)

The predicate is real and comes from `scrobble_sessions`, not from `user_playback.paused_at`
(a saved resume position is not evidence of current playback). The newest non-expired
scrobble session for the user decides: `start`/`scrobble` → watching (returns media type/id,
season/episode ids, progress, `started_at`, `expires_at`); `pause`/`stop`, no session, or an
expired session → `{}` (empty object), matching Trakt. The 5-hour `scrobbleSessionTimeout`
is the ceiling on how long a stale `start` can look live.

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
