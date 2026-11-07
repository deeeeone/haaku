# Integrations Handler (`internal/handlers/integrations`)

## What it does

Implements integration tokens and the inbound event receiver (two files).

**`integrations.go` — token management:**
- `POST /v1/integrations` — create an integration token (`tk_` + 24 random bytes, hex); token value returned **once** on creation; default scope `*`; 201.
- `GET /v1/integrations` — list own tokens with **token values redacted** (only metadata).
- `DELETE /v1/integrations/{id}` — revoke own token (scoped to `user_id`), 204; 404 if not found.
- `LookupToken` — exported helper for the receiver: matches the **blind index** `token_hash` + `active = true`, then decrypts the stored ciphertext (an AAD-swapped or tampered row fails here rather than being used).

**`receiver.go` — inbound event receiver:**
- `POST /v1/integrations/{token}` — authenticates via the token in the path, validates the payload has `event` or `type`, and enqueues an outbox event (`integration.received`) attributed to the token owner; returns 202.

All token-management routes are marked Auth-required in OpenAPI; the receiver route is Auth: none (token in path).

## At rest (issue #16 §2)

An integration token is a bearer credential the receiver looks up on every request, so it needs both halves of the searchable-encryption pattern rather than either one alone:

| Column | Holds | Purpose |
|---|---|---|
| `token_hash` | `HMAC-SHA256(raw, HKDF(root, "tkiller/integration-token-lookup/v1"))`, lowercase hex | The deterministic blind index the lookup matches on. Unique (partial, `WHERE token_hash <> ''`). |
| `token` | `enc:v1:<base64url(nonce‖sealed)>`, AES-256-GCM with `RowAAD("integration_tokens","token",id)` | The recoverable credential, used for display/rotation and decrypted on a successful lookup. |

- **Lookup** matches the blind index, so the raw token never participates in a `WHERE` against storage that could be dumped. A ciphertext never equals a raw token, so there is no plaintext path into the lookup.
- **Rotation** replaces both columns in one update, so the previous token stops matching the moment its blind index is gone.
- **Decision recorded:** the design keeps a recoverable ciphertext (rather than storing only a keyed hash and showing the token once at creation) because the existing contract — returned once, listable, revocable, rotating in place — depends on it. The hash-only variant is strictly more secure and is a product decision for the owner, not a silent change.

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

- **No auth middleware on token routes (RESOLVED)** — `RegisterRoutes` mounted handlers bare; `userID()` used the unchecked `, _` form, so an unauthenticated request yielded an empty user ID and tokens got created under `""` (orphaned) instead of 401. The 3 token-management routes now run `auth.AuthRequired()` and handlers fail closed with 401 on empty user. The receiver route (`RegisterReceiverRoutes`) is deliberately left unauthenticated (token-in-path auth).
- **All failures mapped to 400 (RESOLVED)** — DB-unavailable, generation/create/revoke/queue failures now map to 500 `internal` / 503 `db_unavailable`; only genuine client input mistakes remain 400.
- **DB failure as `badRequest` in receiver (RESOLVED)** — `database unavailable` and queue/token-lookup failures now return 5xx; only malformed payloads are 400.
- **Local error helpers (RESOLVED)** — `writeError`/`badRequest`/`notFound` duplicated with a comment claiming "avoid import cycle with httpapi". The cycle claim was verified real (httpapi/routes.go imports all handlers); the envelope moved to the leaf package `internal/response` (Batch 0) and the locals are gone.
- **Scopes are unvalidated** — arbitrary scope strings accepted; no allowlist or mapping to real permissions. `*` default is broad.
- **No token expiry** — tokens are active until revoked; consider expiry per spec.
- **Token stored in plaintext** — only hashing is absent; revoke is a soft delete flag (`Active`), not a hard delete. Consider hashing tokens at rest and hard-deleting.
- **`handleListTokens`/`handleCreateToken` DB errors as 400 (RESOLVED)** — now 503 on DB-unavailable, 500 on query/write failure.
- **Receiver returns 202 before durable processing** — fine, but the outbox event carries the full payload; check size/retention limits.

## Todos

- [x] Add `auth.AuthRequired` on token management routes; fail closed (401) on empty user. Receiver stays public.
- [x] Map internal failures to 500/503 and only client mistakes to 400. (Batch 6)
- [x] Validate/allowlist scopes; reconsider the `*` default. (Batch 11)
- [x] Decide on token expiry and at-rest hashing. (Batch 12 — DECISION: integration tokens are long-lived receiver credentials with **no expiry**; at-rest hashing is the correct security goal but requires a schema change and is recorded as pending.) Integration tokens authenticate inbound `POST /integrations/{token}` receiver calls — they are capability-style credentials (like API keys / webhook secrets), distinct from short-lived OAuth *access* tokens (which do carry `expires_at` + store only hashes per `data-model.md` oauth_tokens). The `integration_tokens` table (migration 0026) has no `expires_at` column and is not listed as expiring in any spec; revocation is the owner's manual `DELETE /integrations/{id}`. So no expiry is added — expiring a credential the caller must hold indefinitely would break the receiver contract. At-rest hashing: correct to pursue (tokens are returned once then only looked up by value), but `LookupToken` matches on the raw token and adding a hash column is a schema change (needs an append-only migration + `data-model.md` update first per AGENTS) — out of scope for this code-only batch, recorded as a tracked security follow-up rather than guessed around.)
- [x] Replace local error helpers with `internal/response` (import-cycle claim verified; envelope moved to the leaf package).
- [x] Verify module test coverage (`make test`). (Batch 14 — `internal/handlers/integrations/integrations_test.go` covers the `validScopes` allowlist (wildcard excluded), `defaultScopes`, `generateToken`, and `toTokenResponse`. Handler-level integration for the DB-backed token CRUD awaits a DB-driver approval.)
