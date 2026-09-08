# Checkin Handler (`internal/handlers/checkin`)

## What it does

Implements check-in lifecycle endpoints:

- `POST /v1/checkin` — checks the user into a movie, episode, or show. One active checkin per user; any existing non-expired checkin blocks creation (409). Expired ones are cleared before creating. Default expiry is 2 hours.
- `DELETE /v1/checkin` — clears the user's active checkin (204 even if none existed).
- `GET /v1/checkin` — returns the active (non-expired) checkin, or 204 if none.

Key mechanics:
- Uses the **shared auth middleware** (`auth.AuthRequired`) at route registration.
- ID resolution goes through `media.ResolveID` (TMDB id → internal media id + type); episode checkins resolve the season/episode row by season number + episode number.
- Writes go through `cache.WithCacheTransaction` with a `pg_advisory_xact_lock` on the user id (one active checkin per user, race-safe).
- Publishes outbox events (`checkin.created` / `checkin.deleted`) and schedules cache version-key invalidations (user + media).
- Registers OpenAPI route metadata for all three endpoints.

## Completion

| Piece | Status |
|---|---|
| OpenAPI registration for POST/DELETE/GET | Done |
| Shared `auth.AuthRequired` middleware | Done |
| Movie / episode / show resolution via `media.ResolveID` | Done |
| Season + episode row lookup for episode checkins | Done |
| One-active-checkin enforcement (409) | Done |
| Advisory-lock protected writes via cache-tx | Done |
| Cache version-key invalidations (user + media) | Done |
| Outbox events published | Done |
| GET returns 204 when no active checkin | Done |
| Expired-checkin cleanup on create | Done |

## Oversights

- **`writeAPIError` duplicates the envelope** — local helper instead of `internal/httpapi` constructors.
- **Internal errors leak detail** — create/delete return `err.Error()` (e.g. wrapped GORM/advisory-lock messages) as the client-facing message; should be a generic `internal` message with details logged.
- **`resolveCheckinItem` returns raw errors** — the handler maps everything to 400 `invalid_request` via `err.Error()`, so DB failures ("database unavailable", "season not found", "episode not found") surface as 400 instead of 500/404.
- **`409` only on create, not GET/DELETE semantics** — fine, but the "already checked in" detection uses `err.Error() == "already checked in"` string matching; brittle. Better to use a typed sentinel error.
- **Episode response media id** — `response.Episode.IDs.TMDB = int(mediaID)` uses the show's TMDB id, not the episode's; the response references the parent show, which is consistent with Trakt's checkin response but worth verifying against the spec.
- **Delete with zero rows** — DELETE when no checkin exists returns 204 silently; spec should confirm whether 204 is correct for the no-op case.
- **No `sharing` handling** — `checkinSharing` is parsed but never used anywhere.
- **`getUserID` ignores the bool** — a missing `user_id` silently yields empty string, then each handler re-checks for empty and 401s. Redundant double-check with the shared `auth.AuthRequired()` middleware.

## Todos

- [x] Replace `writeAPIError` with `internal/response` error constructors.
- [x] Use a typed sentinel error for "already checked in" instead of string comparison. (Batch 6)
- [x] Map resolve/DB errors to proper status codes (404 for missing season/episode, 500/503 for DB failures) instead of blanket 400. (Batch 6)
- [x] Don't leak wrapped error details in client-facing messages; log them server-side. (Batch 6)
- [x] Decide whether to implement `sharing` or drop the field. (Batch 12 — DECISION: keep the field parsed; it is inert by spec design.) `sync-api.md` includes `sharing?` in the checkin request body ("sharing object controls social cross-post toggles (twitter/mastodon/tumblr analogs — phase 2, we start with none or mastodon)"), and `data-model.md`'s checkins row has a `sharing jsonb` column. So the field is part of the API contract and stays parsed for forward compatibility — but no social cross-post engine exists yet, so it is accepted and currently has no effect. No code change (matching spec's phase-2 note), documented here rather than dropped.)
- [x] Confirm DELETE-no-op and episode response shape against the spec. (Batch 12 — CONFIRMED.) `sync-api.md`: DELETE `/checkin` = "clear active checkin"; the handler returns 204 even when no active checkin exists — a correct idempotent no-op (nothing to clear, nothing deleted). GET returns 204 when no active checkin. Episode checkins resolve through the parent show (per `sync-api.md` media-object shape `{episode:{ids:{tmdb}, season, number}}`) and the POST/GET response follows the spec's `{id, expires_at, watched_at}` shape, embedding the checked-into media. NOTE (pre-existing, not part of this spec decision): the embedded media `ids.tmdb` field carries the resolved *internal* DB id rather than the TMDB id; this is a known representation nuance tracked separately from the spec-shape confirmation here.)
- [x] Verify module test coverage (`make test`). (Batch 14 — `internal/handlers/checkin/checkin_test.go` covers the `mapResolveError` sentinel→status mapping (400/404/503/500), `defaultCheckinDuration`, `ErrAlreadyCheckedIn`, and `getUserID`. Handler-level integration for the DB+Redis checkin flow awaits a DB-driver approval.)
