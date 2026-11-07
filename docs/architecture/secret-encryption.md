# Secret encryption at rest

The rule this document exists to enforce: **a database dump — or a copy of the data directory, a replica, a backup file — must not yield a usable credential.** Anything that can be replayed against a running instance (a bearer token, a signing secret) has to be protected by the root key, not merely by the fact that the database is usually not readable.

This is the security half of running the app on someone's own box: the operator is trusted, the storage is not assumed to be private forever, and the app has no control over where a backup ends up.

## The primitive: `internal/secret`

| Piece | What it is |
|---|---|
| Root key | `TKILLER_SECRET_KEY`, ≥ 32 chars, enforced at boot (missing or short ⇒ the process exits non-zero, per `internal/config`). |
| Derivation | HKDF-SHA256, per **purpose**: `DeriveKey(root, info, n)` where `info` is a versioned label like `tkiller/integration-token-lookup/v1`. A new purpose gets a new label; a changed label is a rotation event, never a silent edit. |
| Cipher | AES-256-GCM with a random nonce per value, `enc:v1:` envelope, base64url payload. |
| AAD binding | `RowAAD(table, column, rowID)` is passed as additional authenticated data, so a ciphertext cannot be moved between rows or columns and still decrypt. This is what makes a partial SQL dump useless rather than merely opaque. |
| Read path | `Decrypt` requires an `enc:v1:` envelope and treats any failure to open it — wrong key, tamper, AAD swap, a value that is not an envelope — as an error. An absent value is an empty column the caller checks before decrypting. |

## What is encrypted today

| Value | Where | Notes |
|---|---|---|
| Webhook signing secrets | `webhook_endpoints` (`secret_hash` for verification + the encrypted secret for signing) | Encrypted at create/rotate; decrypted by the delivery worker when it signs a request. |
| Integration tokens | `integration_tokens.token` | Encrypted at rest, with a **blind index** (`token_hash`) because an inbound request must find the row by the token. See below. |

## What is hashed instead, and why that is correct

| Value | Where | Why a hash is enough |
|---|---|---|
| OAuth client secrets | `0004_create_oauth.secret_hash` | Only ever verified, never displayed or replayed — a hash is the right primitive. |
| OAuth access / refresh tokens | `0004_create_oauth.token_hash`, `refresh_token_hash`, `refresh_token_family` | Presented by the holder and checked by hash; nothing needs the original. |
| Webhook verification secret | `0025_webhooks.secret_hash` | Verification only. |

A credential that is only ever *checked* gets a hash; a credential that must be *found* or *displayed* gets encryption plus a blind index. That distinction is the whole design.

## The blind-index pattern (searchable encryption)

A random-nonce ciphertext cannot be matched with `WHERE token = $1`, so a value that must be looked up needs two columns:

```
token_hash = HMAC-SHA256(raw, HKDF(root, "tkiller/integration-token-lookup/v1"))  -- indexed, deterministic
token      = AES-256-GCM(raw, AAD(table,column,rowID))                            -- enc:v1:…, recoverable
```

- The index is **keyed** (HMAC under a derived key, not a bare SHA-256), so a dump alone gives an attacker nothing to brute-force candidate tokens against — the column is an opaque MAC without the root key.
- The unique constraint lives on the hash (partial, `WHERE token_hash <> ''`), which is the real "a token must not collide" guarantee. A `UNIQUE` on a ciphertext column would be meaningless: every ciphertext is distinct by its random nonce, so it would forbid nothing.
- **The honest trade-off:** a hash-only design (store no recoverable token; show it once at creation) is strictly more secure — even a dump *plus* the root key then recovers nothing — but it makes rotation and display-after-creation impossible. The current shape keeps the recoverable ciphertext because the existing contract (`returned once`, revocable, listable) is a product promise. Changing that is a product decision, not a security patch.

## Key rotation: not implemented

Known gap, tracked in #16. Today a new `info` label is the forward-compatible seam, but there is no re-encrypt pass and no explicit "unsupported in v1" statement to the operator. Rotating the root key currently means re-encrypting by hand.

## Threat model

**Protected against:** a leaked dump, a stolen backup, a readable replica, a curious host operator who copies the data directory — none of those yield a replayable credential without the root key, and AAD binding means they cannot be transplanted row-to-row either.

**Not protected against:** an attacker who has the running process and its environment (the key is in memory to be used), a host operator with root while the app runs, or a root key committed to a repository. The system is designed so the key lives in the environment and never in the database — which is why `TKILLER_SECRET_KEY` is enforced at boot rather than defaulted.

## Sweep: credential-shaped columns in the schema

Checked across `migrations/sql/` for `token|secret|password|api_key|refresh_token|access_token|client_secret|signing|private_key|credential` columns of a text type, as of the integration-token change:

- `0004_create_oauth.sql` — `secret_hash`, `token_hash`, `refresh_token_hash`: hashes. ✔
- `0025_webhooks.sql` — `secret_hash`: hash, with the signing secret encrypted alongside. ✔
- `0026_integration_tokens.sql` — `token`: **was plaintext**, now ciphertext + blind index. ✔
- No other credential-shaped text column exists.

If a future migration adds one, it belongs in this document with the hash-or-encrypt decision recorded.
