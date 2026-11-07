# Local accounts (the self-hosted default)

haaku is an app you run for yourself, so it must be usable on a fresh box with
no third-party identity provider. Identity is therefore **local by default**;
Clerk remains available for the hosted instance and is the option, not the
requirement.

```bash
HAAKU_AUTH_MODE=local          # default: this server issues identity
HAAKU_AUTH_MODE=clerk          # delegate identity to Clerk; the local module is disabled
HAAKU_SIGNUP=approval          # default; approval | open | closed (local mode only)
HAAKU_ADMIN_EMAIL=you@home.lan # owner account, created on the first start of an empty instance
HAAKU_ADMIN_PASSWORD=…         # at least 10 characters; required on first start (local mode)
```

## First start — the owner comes from the environment

**The owner account is created from `HAAKU_ADMIN_EMAIL` / `HAAKU_ADMIN_PASSWORD`
at startup, on an empty instance, before the API answers anything.** It is
`active`, `is_admin`, has a `local_` id, and its password is hashed with the same
Argon2id path as any other account. It can log in immediately with those
credentials.

There is no longer a "whoever signs up first becomes the owner": a fresh,
reachable instance used to hand ownership to the first stranger who found the
sign-up page. Now the operator holds the owner credentials *before* the box is
reachable.

The rules, in one place:

- **Empty instance, both variables set** → the owner is created. Safe on every
  start afterwards: seeding is idempotent.
- **Empty instance, variables unset** → the local-mode server **refuses to
  start**, naming the variables and what to do. An instance nobody can
  administer is worse than a loud exit. The message is:

  > `this is a fresh local-mode instance with no accounts, and no owner is
  > configured: set HAAKU_ADMIN_EMAIL and HAAKU_ADMIN_PASSWORD so the first
  > start can create the owner account, or set HAAKU_AUTH_MODE=clerk to delegate
  > identity to Clerk`

- **Instance that already has accounts** → never touched, whatever the variables
  say. A second start does not add, modify or re-hash anything.
- **Clerk mode** → both variables are **ignored entirely**. Clerk owns identity,
  and writing a local owner row would create the "local user shadows a Clerk
  user" situation the mode switch exists to prevent.
- **Migrate-only** (`HAAKU_MODE=migrate` / `--migrate-only`) never bootstraps and
  never refuses — so CI's migrate-only jobs and the image's first migration run
  are unaffected.

The password is validated against the same minimum length as the sign-up route
(10 characters) and a failure names `HAAKU_ADMIN_PASSWORD`; the value itself is
never logged. `HAAKU_ADMIN_EMAIL` must be a valid address.

## What the mode switch actually does

`HAAKU_AUTH_MODE=clerk` **unregisters** the module: `auth.RegisterLocalAuthRoutes`
returns before touching the router *or* the OpenAPI registry, so
`openapi.yaml` — and the route inventory the spec gate compares against — does
not contain `/v1/auth/local/*` or `/v1/admin/*` at all. The auth middleware also
stops accepting `hs_` tokens in clerk mode, so the two systems can never both
authenticate one instance. There is no dual-write and no "local user shadows a
Clerk user".

The one route that exists in both modes is `GET /v1/auth/config`, which is how a
client learns which authenticator to use:

```json
{
  "mode": "local",
  "local_auth": true,
  "signup_policy": "approval",
  "first_run": true,
  "password_min_length": 10,
  "password_kdf": "argon2id",
  "password_reset_delivery": "admin_handover",
  "session": { "token_prefix": "hs_", "transport": "bearer", "ttl_seconds": 2592000 }
}
```

In clerk mode it reports `"mode": "clerk", "local_auth": false` and the session
block is empty.

`first_run` is `true` only while an instance has no accounts at all. Since a
local-mode instance now refuses to serve without an owner (above), it is `false`
on every running local instance — the field stays for the client contract, but
the "create the owner account" framing it used to drive no longer applies: the
owner already exists.

## Sign-up policy — stated plainly

**The owner is created from the environment at startup (above). Every sign-up
after that is an ordinary account and follows `HAAKU_SIGNUP` directly —
`approval` (the default) creates the account `pending`: it cannot log in, and its
signup response contains no session, until an admin approves it in the panel.**
This means a box that happens to be reachable does not have an open door, and
adding a household member is one click in the admin panel rather than a config
change. `open` activates new accounts immediately; `closed` refuses every
sign-up, because the owner already exists. If the default is wrong for a
household, it is one variable.

(First-run ownership — "the first account on an empty instance becomes the
owner" — is **removed**. It is now the environment that decides who owns the
instance, not arrival order.)

## Identity and id namespacing

`users.id` stays a text primary key. A local account's id is
`local_` + 32 hex characters (16 random bytes); Clerk ids are always
`user_...`. The prefix makes the two namespaces disjoint by construction, so a
local id can never collide with, or be mistaken for, a Clerk id. Local signup
claims its email under a **partial** unique index on `lower(email)` restricted
to `left(id, 6) = 'local_'`, so Clerk-mirrored rows are unaffected and a
soft-deleted local account releases the claim.

Schema (append-only, `migrations/sql/*_local_auth.sql`): `local_credentials`,
`local_sessions`, `local_auth_tokens`. None of them touches
`users.clerk_sync_status` / `users.clerk_synced_at`.

## Passwords

Argon2id from the pinned `golang.org/x/crypto` dependency, stored as PHC strings
(`$argon2id$v=19$m=19456,t=2,p=1$…`) so the parameters travel with the hash and
can be raised later without invalidating passwords. Chosen over bcrypt because
it is memory-hard (19 MiB per hash removes most of the GPU/ASIC advantage of
offline attack) and because bcrypt truncates input at 72 bytes. The 19 MiB
parameter is the OWASP low-memory profile: a self-hosted box is often a small
ARM board, and a 64 MiB hash would make a handful of concurrent logins on one
core an easy denial of service.

## Sessions

A session is a **row**, not a signed token — because banning a user has to kill
the sessions they are already holding, and only a server-side row can be
revoked.

- The client receives `hs_` + 32 random bytes (base64url). It is never stored.
- `local_sessions.token_hash` is an HMAC-SHA256 **blind index** of the token,
  keyed with `secret.DeriveKey(HAAKU_SECRET_KEY, "haaku/local-session-token/v1", 32)`.
  Same root key, same HKDF helper, a different purpose label from the at-rest
  data key — no second key, no second crypto scheme.
- Every request re-checks `revoked_at`, `expires_at` and the account status, so
  a ban takes effect on the next request, not at token expiry. `last_seen_at`
  (and `users.last_active_at`) is refreshed at most every five minutes.
- TTL is 30 days. A password reset revokes every session for that user.

## Password reset

Local instances have no mail guarantee, so reset is an **admin action**: the
admin calls `POST /v1/admin/users/{id}/password-reset`, gets a one-time `hr_`
token back **once**, and hands it to the user out of band. The token is stored
hashed, lives 24 hours, and is redeemed at `POST /v1/auth/local/password-reset`
with the new password. Redemption is a conditional `UPDATE … WHERE
consumed_at IS NULL`, so exactly one of two concurrent redemptions can win.

The response says `"delivery": "admin_handover"` and this build has no mail
transport, so nothing silently depends on SMTP. There is no "email me a reset
link" endpoint; adding one means adding a mailer first.

## Rate limiting

Login attempts are counted in Redis with the same ZSET sliding window as the
request and report limiters, and fail **open** if Redis is unavailable:
20 attempts / 5 min per client IP and 10 / 5 min per email (the email is counted
as a truncated SHA-256, never stored). Exceeding either answers `429` with
`Retry-After`.

## Endpoints (local mode)

| Method | Path | Auth |
|---|---|---|
| GET | `/v1/auth/config` | none — mode discovery, present in both modes |
| POST | `/v1/auth/local/signup` | none |
| POST | `/v1/auth/local/login` | none |
| POST | `/v1/auth/local/logout` | required (revokes the token used) |
| POST | `/v1/auth/local/password-reset` | none (the one-time token is the credential) |
| GET | `/v1/admin/users` | admin |
| POST | `/v1/admin/users/{id}/ban` | admin |
| POST | `/v1/admin/users/{id}/unlock` | admin |
| POST | `/v1/admin/users/{id}/approve` | admin |
| POST | `/v1/admin/users/{id}/password-reset` | admin |

Admin means `users.is_admin` or the `ADMIN_USER_IDS` allowlist — a moderator is
not an admin (`auth.RequireAdmin`, 403 otherwise). Every response uses the
standard `{"error":{"code","message"}}` envelope.

## Contract for the frontend

- **Transport is a bearer token, not a cookie.** Store the `session.token` from
  signup/login and send `Authorization: Bearer hs_…` on every request. There is
  no CSRF surface because nothing is sent ambiently, and the existing
  `auth.AuthRequired` middleware already reads that header — the module issues
  identity, it does not reshape the middleware stack.
- **Learn the mode from `GET /v1/auth/config`** before rendering a login form:
  `mode: "clerk"` means the local form must not be shown (it would 404).
- **Logout** is `POST /v1/auth/local/logout` with the bearer token; the server
  revokes the row, so the client should also discard the token.
- **Signup** (`201`) returns `status` (`active` | `pending`) and `session`
  (null for pending). Show the pending message from `message` rather than
  guessing.
- **Login errors**: `401 unauthorized` for both unknown email and wrong
  password (deliberately indistinguishable), `403 account_pending`,
  `403 account_banned`, `429 rate_limited` with `Retry-After`.
- **Reset**: the admin panel shows `token` + `url_path` once; the reset screen
  posts `{token, new_password}` to `/v1/auth/local/password-reset`.

## Deliberately out of scope

No SMTP transport, no email verification, no 2FA, no refresh tokens, no
"remember me" toggle, no RBAC beyond admin/moderator, and no single-session
limit. The admin panel is the list plus the four actions above — not a general
account console.