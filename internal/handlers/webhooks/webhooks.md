# Webhooks Handler (`internal/handlers/webhooks`)

## What it does

User-owned webhook endpoint management (delivery worker itself lives elsewhere):

- `GET /v1/webhooks` — list own endpoints (owner-scoped).
- `POST /v1/webhooks` — create; validates URL (HTTPS + DNS + **SSRF guard**), event allowlist from `ValidEventTypes()`, generates or accepts a secret; **secret returned once**.
- `PUT /v1/webhooks/:id` — update url/events/active (re-validated).
- `DELETE /v1/webhooks/:id` — delete (204).
- `POST /v1/webhooks/:id/rotate-secret` — rotate; new secret returned once.
- `POST /v1/webhooks/:id/test` — queues a `ping` event via outbox (202).
- `GET /v1/webhooks/:id/deliveries` — last 100 `WebhookDelivery` rows.

Also exports the delivery building blocks for the worker: `EventCatalogV1` (event→wire-type map), `WebhookPayload`/`NewWebhookPayload`, and `GenerateHMAC`/`VerifyHMAC` (timestamped HMAC-SHA256 signature scheme). OpenAPI registered.

## Completion

| Piece | Status |
|---|---|
| OpenAPI registration (7 routes) | Done |
| CRUD + rotate-secret + test ping | Done |
| URL/HTTPS validation + SSRF DNS guard | Done |
| Event allowlist (outbox constants) | Done |
| Delivery log (last 100) | Done |
| HMAC helpers + payload/event catalog | Done |

## Oversights

- **No auth middleware on routes (RESOLVED)** — `RegisterRoutes` (webhooks.go:124-132) attached nothing and `userID(c)` ignored the type-assert `ok` (webhooks.go:204-207), so unauthenticated requests silently operated with `user_id=""` (empty lists / endpoints owned by the empty user) instead of 401. All 7 routes now run `auth.AuthRequired()`; a new `requireUserID` helper fails closed with 401 when the user is missing, replacing the lax `userID` reads.
- **SSRF guard has DNS-rebinding gap (RESOLVED)** — `isValidHTTPS` resolved the host to check for private/loopback IPs, but the delivery worker re-resolved at dial time, so a rebinding host could pass validation and later point at an internal IP. Now both validation and delivery share `ResolvePublicIP`/`isBlockedIP`; the worker pins the verified IP in a custom `DialContext` (no re-resolution), refuses to follow redirects (`CheckRedirect` → `ErrUseLastResponse`), rejects userinfo/non-HTTPS URLs, and defaults the dial port to 443.
- **`hashSecret` broke end-to-end verification (RESOLVED)** — stored a keyed HMAC of the secret and the worker signed deliveries with that stored value, but consumers verify with the plaintext shown once (`GenerateHMAC`), so signatures never matched. Also the "key" was a public static string, i.e. an offline-brute-forceable SHA-256. Now secrets are encrypted at rest with AES-256-GCM (`EncryptSecret`/`DecryptSecret`, server key from `TKILLER_SECRET_KEY`), the worker decrypts and signs with the plaintext, and consumers' `VerifyHMAC(plaintext)` works end-to-end. Note: this contradicts the planning spec's "hashed at rest" line — flag for a planning-DB note (hash can't be recovered; encryption can).
- **Webhook crypto moved onto `internal/secret` (issue #16 §2)** — `EncryptSecret`/`DecryptSecret` now live in `secret.go` and delegate to the shared `trakt-api/internal/secret` package: HKDF-SHA256 over the root key (`TKILLER_SECRET_KEY`) → AES-256-GCM, envelope `enc:v1:<base64url(nonce‖sealed)>`. Each ciphertext is AAD-bound to `webhook_endpoints:secret_hash:<id>`, so a secret moved to another row fails to decrypt (the create handler now inserts, then encrypts under the assigned id, in one transaction). The read path is strict: an empty stored value is treated as an absent secret, and any other value must be a valid `enc:v1:` envelope — a failure to decrypt is an error, never a fallback to raw bytes. `SecretKeyFromEnv` is gone; the root key is loaded and validated by `internal/secret`.
- **Error helpers "avoid import cycle" claim (RESOLVED)** — the cycle claim was real: `internal/httpapi/routes.go` imports every handler package to register routes, so a handler importing `httpapi` forms a cycle. The envelope was moved to the leaf package `internal/response` (Batch 0); `webhooks.go` now imports it directly and the local `writeError`/`badRequest`/`notFound`/`serverError`/`serviceUnavailable` helpers are gone.
- **DB errors mapped to 400 (RESOLVED)** — `"database unavailable"` is now 503 `db_unavailable` and query failures are 500 `internal` (matching `reference`/`reports`/`stats` and api-conventions §9); only genuine client errors stay 400.
- **`handleDeliveries` hard-capped at 100 (RESOLVED)** — now paginated via `?page&limit` (default 25, max 100) with the §7 headers (`X-Pagination-*` + `Link: rel="next"`) and a stable `created_at DESC, id DESC` order.
- **No cache-tx on create/update/delete/rotate** — plain `db` writes; test-ping is the only mutation that emits an outbox event. No cache invalidation for the list/deliveries reads.
- **`handleDeliveries` capped at 100 with no pagination** — hard limit, no page/limit params.
- **Test ping doesn't validate active state or record a delivery** — `ping` outboxed regardless of `ep.Active`; no delivery-log row is created by this handler (the worker presumably does).
- **`GenerateHMAC` has no timestamp-window check** — the format is `timestamp.body`, but the verification helper just compares bytes; replay protection (clock skew window) must live in the worker and isn't enforced here.
- **`NewWebhookPayload` ignores the passed `eventID` (RESOLVED)** — the parameter was dead; the function always generated a fresh `uuid.New()`. Now it takes only `(eventType, data)`.
- **`endpointResponse` returns raw `time.Time` (RESOLVED)** — `endpointResponse`/`deliveryResponse` timestamps now emit `.000Z` via `response.FormatTime` (Batch 3).

## Todos

- [x] Attach `auth.AuthRequired` to all routes and make user reads fail closed (`requireUserID`, 401).
- [x] Harden SSRF: pin resolved IPs at dial time (ResolvePublicIP), reject userinfo/non-HTTPS, stop following redirects at delivery. Shared `isBlockedIP`/`ResolvePublicIP` helpers in the webhooks package + unit tests.
- [x] Replace `hashSecret` with AES-256-GCM encryption at rest (`TKILLER_SECRET_KEY`); worker decrypts and signs with the plaintext so consumer verification works. Flag "hashed at rest" spec wording for a planning-DB update.
- [x] Unify webhook-secret crypto behind `internal/secret` (issue #16 §2): HKDF-derived data key, `enc:v1:` envelope, AAD row binding to `webhook_endpoints:secret_hash:<id>`, strict read path, required root key at startup.
- [x] Map DB failures to 503/500, not 400.
- [x] Add pagination to `/deliveries` (page/limit, §7 headers, stable order).
- [x] Remove the dead `eventID` parameter from `NewWebhookPayload`.
- [x] Move error envelope to a leaf package (`internal/response`) so handlers can share it — unblocked the httpapi→handlers import cycle.
- [ ] Verify module test coverage (`make test`).
