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
- **`hashSecret` is a keyed HMAC with a public static key** — `hmac.New(sha256.New, []byte(secret))` with fixed message `"webhook-secret"` (webhooks.go:217-221) is effectively a plain SHA-256 of the secret; anyone who knows the scheme can brute-force it offline. Use a server-side pepper or proper KDF. Also contradicts "secrets in .env" convention (a static literal key).
- **Error helpers claim "avoid import cycle"** — `internal/httpapi` does not import handlers (routes.go imports handlers), so `httpapi` helpers could be used; the local `writeError`/`badRequest`/`notFound` duplication is likely unjustified.
- **DB errors mapped to 400** — `"database unavailable"`/`"failed to list webhooks"` all return 400 `invalid_request`; should be 503/500 per convention.
- **No cache-tx on create/update/delete/rotate** — plain `db` writes; test-ping is the only mutation that emits an outbox event. No cache invalidation for the list/deliveries reads.
- **`handleDeliveries` capped at 100 with no pagination** — hard limit, no page/limit params.
- **Test ping doesn't validate active state or record a delivery** — `ping` outboxed regardless of `ep.Active`; no delivery-log row is created by this handler (the worker presumably does).
- **`GenerateHMAC` has no timestamp-window check** — the format is `timestamp.body`, but the verification helper just compares bytes; replay protection (clock skew window) must live in the worker and isn't enforced here.
- **`NewWebhookPayload` ignores the passed `eventID`** — it takes `eventID` but always generates a fresh `uuid.New()` (webhooks.go:617-624); the parameter is dead.
- **`endpointResponse` returns raw `time.Time`** (RFC3339 in JSON) — fine, but inconsistent with the `.000Z` literals used across other modules.

## Todos

- [x] Attach `auth.AuthRequired` to all routes and make user reads fail closed (`requireUserID`, 401).
- [x] Harden SSRF: pin resolved IPs at dial time (ResolvePublicIP), reject userinfo/non-HTTPS, stop following redirects at delivery. Shared `isBlockedIP`/`ResolvePublicIP` helpers in the webhooks package + unit tests.
- [ ] Replace `hashSecret` with a server-peppered KDF (no static public key).
- [ ] Map DB failures to 503/500, not 400.
- [ ] Add pagination to `/deliveries`.
- [ ] Use `internal/httpapi` helpers (cycle claim looks wrong — verify).
- [ ] Remove the dead `eventID` parameter from `NewWebhookPayload`.
- [ ] Verify module test coverage (`make test`).
