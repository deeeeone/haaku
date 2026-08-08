# Integrations Handler (`internal/handlers/integrations`)

## What it does

Implements integration tokens and the inbound event receiver (two files).

**`integrations.go` — token management:**
- `POST /v1/integrations` — create an integration token (`tk_` + 24 random bytes, hex); token value returned **once** on creation; default scope `*`; 201.
- `GET /v1/integrations` — list own tokens with **token values redacted** (only metadata).
- `DELETE /v1/integrations/{id}` — revoke own token (scoped to `user_id`), 204; 404 if not found.
- `LookupToken` — exported helper for the receiver: matches token + `active = true`.

**`receiver.go` — inbound event receiver:**
- `POST /v1/integrations/{token}` — authenticates via the token in the path, validates the payload has `event` or `type`, and enqueues an outbox event (`integration.received`) attributed to the token owner; returns 202.

All token-management routes are marked Auth-required in OpenAPI; the receiver route is Auth: none (token in path).

## Completion

| Piece | Status |
|---|---|
| OpenAPI registration (3 + 1 routes) | Done |
| Token generation (crypto/rand, prefixed) | Done |
| Token value returned once | Done |
| Token listing with redaction | Done |
| Revoke scoped to owner + 404 semantics | Done |
| `LookupToken` for receiver auth | Done |
| Receiver payload validation | Done |
| Outbox publish on receive | Done |

## Oversights

- **No auth middleware on token routes** — `RegisterRoutes` mounts handlers bare; relies on `user_id` Local set elsewhere. `userID()` uses the unchecked `, _` form, so an unauthenticated request yields an empty user ID and tokens get created under `""` (orphaned) rather than 401. Must confirm a global auth layer exists; otherwise routes are unauthenticated.
- **All failures mapped to 400** — DB unavailable, generation failure, create failure all return `bad_request` instead of 500/503 `internal`; misuses the 400 status code.
- **DB failure as `badRequest` in receiver** — `database unavailable` and queue failures return 400 (client error) rather than 5xx.
- **Local error helpers** — `writeError`/`badRequest`/`notFound` duplicated with a comment claiming "avoid import cycle with httpapi", but other handler packages use httpapi helpers fine; the cycle concern should be verified.
- **Scopes are unvalidated** — arbitrary scope strings accepted; no allowlist or mapping to real permissions. `*` default is broad.
- **No token expiry** — tokens are active until revoked; consider expiry per spec.
- **Token stored in plaintext** — only hashing is absent; revoke is a soft delete flag (`Active`), not a hard delete. Consider hashing tokens at rest and hard-deleting.
- **`handleListTokens`/`handleCreateToken` DB errors as 400** — same as above.
- **Receiver returns 202 before durable processing** — fine, but the outbox event carries the full payload; check size/retention limits.

## Todos

- [ ] Confirm/add `auth.AuthRequired` on token management routes (and prevent empty-user token creation).
- [ ] Map internal failures to 500/503 and only client mistakes to 400.
- [ ] Validate/allowlist scopes; reconsider the `*` default.
- [ ] Decide on token expiry and at-rest hashing.
- [ ] Replace local error helpers with `internal/httpapi` (verify the import-cycle claim).
- [ ] Verify module test coverage (`make test`).
