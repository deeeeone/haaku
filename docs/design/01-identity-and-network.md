# 01 — Identity, accounts and the social network

Status: **decided** (Pas, 2026-11-07). Supersedes the open identity question in issue #8 ("keep Clerk vs built-in accounts") and refines the boundary sketch in issue #54.

## The decision in one paragraph

One build, two independent config toggles. Haaku is primarily a self-hosted tracker that runs for one household; the instance we host on the official domain is the same build with different config. Identity is either **local** (accounts and admin live on the instance) or **Clerk** (accounts live in Clerk). The social network is a separate concern with its own toggle: the official hosted instance *is* the network, and self-hosted instances may optionally **piggyback** it — each local user can link their own account so their public lists and usage can be shared and pulled. A self-hosted instance that does not enable that toggle never makes an outbound call and never nags; every local feature works forever without it.

## Two orthogonal toggles

This is the part worth getting right: **auth mode and social are not the same axis**, and neither implies the other.

| Toggle | Values | Default | What it controls |
|---|---|---|---|
| `TKILLER_AUTH_MODE` | `local` \| `clerk` | `local` | Who issues identity. `local` enables the in-app account module (signup/login/sessions/admin panel, issue #53); `clerk` disables that module entirely and delegates to Clerk. |
| `TKILLER_SOCIAL_MODE` | `off` \| `host` \| `client` | `off` | Whether this instance speaks to the social network, and in which direction. `host` = this instance runs the network (official domain). `client` = this instance points at a network host and piggybacks it. `off` = no social, no outbound calls. |

Any combination is legal and each has a coherent story:

- `local` + `off` — the plain self-hosted tracker. Nothing outbound except the metadata provider the operator configured.
- `local` + `client` — a self-hosted household that wants the social features. Local accounts for the household; individual users link their own network accounts to share/pull.
- `clerk` + `host` — the official instance. Clerk identity, network authority.
- `clerk` + `off` — a hosted instance with social disabled (useful for staging, and a proof the axis really is orthogonal).

## Accounts

- **Local (`TKILLER_AUTH_MODE=local`)** — the in-app module. Signup, login, logout, sessions derived from `TKILLER_SECRET_KEY` (the existing root key, via `internal/secret`'s per-purpose HKDF — no second key, no second crypto scheme), plus a deliberately small admin panel: a table of users (email, username, created, last active, status) with ban/lock, unlock and "send password reset". First account on an empty instance becomes the owner/admin; signup policy after that is config. Multi-user is expected — several people on the same box, each with their own history and lists. Full scope in issue #53.
- **Clerk (`TKILLER_AUTH_MODE=clerk`)** — Clerk owns accounts, signup, password reset and the user directory; the local module is **disabled** (routes not registered, so the OpenAPI inventory shows it). That is what the official instance runs; TheIntroDB's setup is the reference for how clean it should be.
- The two never both authenticate the same instance. There is no dual-write and no local account shadowing a Clerk account.

Both modes must produce the same downstream contract: `c.Locals("user_id")` and the existing `AuthRequired` / `PrivacyFilterSection` / `RequireModerator` middleware keep working untouched. Identity is issued above them, not inside them.

## The social network

- The official hosted instance runs the network (`SOCIAL_MODE=host`). Users who authenticate there with Clerk are **automatically** part of the social network — no separate opt-in, because that instance *is* the community.
- A self-hosted instance may set `SOCIAL_MODE=client` and point at a network host. Then each **local user** may individually link their account and decide what to share. Linking is per-user, revocable, and never implied by the instance's own configuration.
- Linking flow: a user on the self-hosted instance starts the link, the instance issues a short-lived PIN (or a redirect through Clerk if the instance is Clerk-mode), the user confirms it while signed in on the network, and the network binds the pair and returns a scoped user token plus the network account id. The instance's own credential is separate from the user's token — see below.

### Same API paths, two backends

The implementation shape Pas proposed and this doc records: **the social surface is one set of API paths with two backends behind one interface.**

- On the network host, the backend is local — the routes read and write the network's own database, exactly like any other endpoint in this repo.
- On a connected self-hosted instance, the backend is **remote**: the same routes proxy the equivalent call to the network host and stream the answer back. The instance does not grow a second social data model.
- `SOCIAL_MODE=off` selects a third, no-op backend whose routes are not registered at all.

Consequences to honour:

- The local implementation is the reference; the remote one is a thin client with the same request/response shapes, so the two cannot drift in shape without a test noticing.
- A network outage must degrade **one** way: social routes fail with a clear, documented error, and every local tracker feature keeps working. No local feature may depend on the network being up.
- The proxy is one interface with a no-op implementation, so the default self-hosted build never links the social client in by accident.

### What crosses the boundary

- **Pull is the default direction.** Public lists and public usage from the network. This is what makes a self-hosted instance feel connected without exporting anything.
- **Push is explicit and per-item.** A user's public lists, and aggregate counts, and only when they turned it on: gated by `users.is_private` as the master override plus the per-section privacy flags (design of record: issue #48, now enforced), itemised, inspectable, and revocable. Never a default-on background sync of everything.
- **Identity across the boundary:** the instance authenticates as *the instance* (its own credential, issued when the operator enables `client` mode and stored encrypted with `internal/secret`), while the human is identified by their network account. Both exist and must stay distinct concepts — losing that distinction is how "your box phones home as you" bugs happen.
- **What must never cross:** the root key, local credentials/password hashes, private-by-flag data, other household members' data (linking is per user and never implicit for the household), and anything the operator's instance holds on behalf of someone who never linked.

## Non-goals (v1)

- Instance-to-instance federation without a network host. v1 piggybacks; direct peering is a different (harder) product.
- A federated identity for local accounts. Local accounts are local; linking is what connects them.
- Any social feature that becomes required for local tracking to work.
- Anonymous telemetry of any kind.

## Open questions

These need Pas (or a follow-up issue) before implementation of the client half:

1. **PIN logistics** — expiry window, whether the PIN is displayed in the instance UI, re-entry on failure, and whether a Clerk-authenticated self-hosted instance should prefer a redirect over a PIN. *Pas decides.*
2. **Where aggregates are computed** — the network computing cross-instance `watchers`/`plays`/`collectors` from pushed counts, versus instances pushing raw events. Aggregate-only is cheaper and leaks less; it is also less useful. *Pas decides.*
3. **Unlink and ban semantics** — what happens to already-pushed public data when a user unlinks, is banned locally, or deletes their account: purge on unlink, or retain as "shared at the time"? *Pas decides.*
4. **Moderation and abuse on the network** — reports, takedowns and rate limits become a hosted-service concern the moment anyone can push. Ties into issue #10. *Tracked in #10.*
5. **Protocol versioning** — an instance and a network host will drift. Minimum-supported-version handshake, and what an old instance sees (documented error, not silent breakage). *Decide before the client ships.*

## Where this is implemented

| Piece | Issue |
|---|---|
| Local account module (default auth, admin panel) | #53 |
| Network boundary, toggles and the proxy backend | #54 |
| Privacy flags that gate push (`is_private` master override) | #48 — landed |
| Social features themselves (follows, comments, feed, cross-instance lists) | not scheduled; they are the network's product, not the tracker's |
