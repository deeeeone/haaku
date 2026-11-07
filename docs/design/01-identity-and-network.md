# 01 — Identity, accounts and the social network

Status: **decided** (Pas, 2026-11-07). Supersedes the open identity question in issue #8 ("keep Clerk vs built-in accounts") and refines the boundary sketch in issue #54.

## The decision in one paragraph

One build, two independent config toggles. Haaku is primarily a self-hosted tracker that runs for one household; the instance we host on the official domain is the same build with different config. Identity is either **local** (accounts and admin live on the instance) or **Clerk** (accounts live in Clerk). The social network is a separate concern with its own toggle: the official hosted instance *is* the network, and self-hosted instances may optionally **piggyback** it — each local user can link their own network account so their public lists and watch data can be shared and pulled. A self-hosted instance that does not enable that toggle never makes an outbound call and never nags; every local feature works forever without it.

## Two orthogonal toggles

**Auth mode and social are not the same axis**, and neither implies the other.

| Toggle | Values | Default | What it controls |
|---|---|---|---|
| `TKILLER_AUTH_MODE` | `local` \| `clerk` | `local` | Who issues identity. `local` enables the in-app account module (signup/login/sessions/admin panel, issue #53); `clerk` disables that module entirely and delegates to Clerk. |
| `TKILLER_SOCIAL_MODE` | `off` \| `host` \| `client` | `off` | Whether this instance speaks to the social network, and in which direction. `host` = this instance runs the network (official domain). `client` = this instance points at a network host and piggybacks it. `off` = no social, no outbound calls. |

Any combination is legal and each has a coherent story:

- `local` + `off` — the plain self-hosted tracker. Nothing outbound except the metadata provider the operator configured.
- `local` + `client` — a self-hosted household that wants the social features. Local accounts for the household; individual users link their own network accounts to share and pull.
- `clerk` + `host` — the official instance. Clerk identity, network authority.
- `clerk` + `off` — a hosted instance with social disabled (staging; also proof the axis really is orthogonal).

## Accounts

- **Local (`TKILLER_AUTH_MODE=local`)** — the in-app module. Signup, login, logout, sessions derived from `TKILLER_SECRET_KEY` (the existing root key, via `internal/secret`'s per-purpose HKDF — no second key, no second crypto scheme), plus a deliberately small admin panel: a table of users (email, username, created, last active, status) with ban/lock, unlock and "send password reset". First account on an empty instance becomes the owner/admin; signup policy after that is config. Multi-user is expected — several people on the same box, each with their own history and lists. Full scope in issue #53.
- **Clerk (`TKILLER_AUTH_MODE=clerk`)** — Clerk owns accounts, signup, password reset and the user directory; the local module is **disabled** (routes not registered, so the OpenAPI inventory shows it). That is what the official instance runs; TheIntroDB's setup is the reference for how clean it should be.
- The two never both authenticate the same instance. There is no dual-write and no local account shadowing a Clerk account.

Both modes must produce the same downstream contract: `c.Locals("user_id")` and the existing `AuthRequired` / `PrivacyFilterSection` / `RequireModerator` middleware keep working untouched. Identity is issued above them, not inside them.

## The social network

- The official hosted instance runs the network (`SOCIAL_MODE=host`). Users who authenticate there with Clerk are **automatically** part of the social network — no separate opt-in, because that instance *is* the community.
- A self-hosted instance may set `SOCIAL_MODE=client` and point at a network host. Then each **local user** may individually link their account and decide what to share. Linking is per-user, revocable, and never implied by the instance's own configuration.
- **Linking is a Clerk redirect** (decided): the self-hosted user is sent to the network, signs in there with their own Clerk account, confirms the link, and the network hands back a scoped user token plus the network account id. There is no PIN/device-code flow — a self-hosted user who wants social features needs a network Clerk account, and that is the point at which they exist on the network at all.

### Server-side, so it works with the browser closed

The instance holds the linked user's network credential **server-side** and talks to the network on their behalf. That is deliberate: a self-hosted instance must be able to keep shared state in sync while nobody has the web UI open — flushing a share that was queued when the box was offline, pulling a list someone else updated. A browser-only design cannot do that, and the tracker is expected to run unattended.

Two consequences, both non-negotiable:

1. **Everything sensitive is encrypted at rest** — the user's network token, the instance credential, webhook/integration secrets — using `internal/secret`'s root-key-derived envelope with AAD row-binding. A database dump must not yield a usable credential (issue #16's bar).
2. **The trust model is stated plainly to the user**, because it is real: whoever hosts the instance can, in principle, act on the network as any user linked to it. That is inherent to server-side sync, not an implementation flaw.

### The trust model, in the product's own words

> Hosting your own instance means you trust its operator — if that is you, fine; if it is someone else, know that they can act as any account linked to that instance. This is why the hosted option exists: on the official instance you authenticate with Clerk directly and the operator is us.

The docs and the linking UI must say that, not hide it. It is the honest version of "self-hosted means you are in control" — you are in control of the box, and the box is a trusted party.

### Same API paths, two backends

**The social surface is one set of API paths with two backends behind one interface.**

- On the network host, the backend is local — the routes read and write the network's own database, exactly like any other endpoint in this repo.
- On a connected self-hosted instance, the backend is **remote**: the same routes proxy the equivalent call to the network host and stream the answer back. The instance does not grow a second social data model.
- `SOCIAL_MODE=off` selects a third, no-op backend whose routes are not registered at all.

Consequences to honour:

- The local implementation is the reference; the remote one is a thin client with the same request/response shapes, so the two cannot drift in shape without a test noticing.
- Outbound work is a **job, not a request**. A share queued while offline is flushed by the worker (issue #52's worker mode, and the outbox pattern already in the repo), so the feature survives restarts and network outages without holding a user request open.
- A network outage must degrade **one** way: social routes fail with a clear, documented error, and every local tracker feature keeps working. No local feature may depend on the network being up.
- The proxy is one interface with a no-op implementation, so the default self-hosted build never links the social client in by accident.

### What crosses the boundary

- **Users choose what to share, per thing.** The initial set: **watch data** and **lists**, plus anything added later (comments are the likely next one — the types must be a list we can extend, not a hardcoded pair). Sharing is per-item, inspectable, and revocable.
- Sharing is gated by `users.is_private` as the master override plus the per-section privacy flags (design of record: issue #48, now enforced). A private-by-flag item does not leave the box, whatever else is enabled.
- **Retention on unlink (decided): the network retains what was shared as "shared at the time" and shows the contributor as unlinked.** The community's history stays coherent — a list other people saved does not rot because its author left. Banning a user locally does *not* retroactively purge the network; deletion of an account locally does not either. If a user wants their shared data gone, that is an explicit network-side delete action, and it should be offered in the UI rather than left as an ambiguity.
- **Identity across the boundary:** the instance authenticates as *the instance* (its own credential, issued when the operator enables `client` mode, stored encrypted), while the human is identified by their network account and acted on with that user's server-side token. Both exist and must stay distinct concepts.
- **What must never cross:** the root key, local credentials/password hashes, private-by-flag data, other household members' data (linking is per user and never implicit for the household), and anything the operator's instance holds on behalf of someone who never linked.

## Non-goals (v1)

- Instance-to-instance federation without a network host. v1 piggybacks; direct peering is a different (harder) product.
- A federated identity for local accounts. Local accounts are local; the Clerk link is what connects a person to the network.
- Any social feature that becomes required for local tracking to work.
- Anonymous telemetry of any kind.

## Decided open questions

- **PIN logistics** — dropped. Linking is a Clerk redirect (see above).
- **Where aggregates are computed** — resolved by the push list: the network receives what users share (watch data, lists, later types), not just rolled-up counts.
- **Unlink and ban semantics** — retain as "shared at the time", show as unlinked; offer an explicit delete for the user who wants their data removed.
- **Protocol versioning** — an engineering decision, not a product one: instances send their protocol version on every call, the network enforces a minimum supported version, and an instance below it gets a documented, actionable error rather than silent breakage. Compatible additions only within a major version.

## Still open

1. **Network moderation and abuse** — reports, takedowns and rate limits become a hosted-service concern the moment anyone can push. Ties into issue #10. *Tracked in #10.*
2. **The exact share-type registry** — the initial set is watch data + lists; comments and later types need a registry (with per-type privacy defaults) rather than a growing pair of booleans. *Design when the first post-launch type is proposed.*

## Where this is implemented

| Piece | Issue |
|---|---|
| Local account module (default auth, admin panel) | #53 |
| Network boundary, toggles and the proxy backend | #54 |
| Privacy flags that gate sharing (`is_private` master override) | #48 — landed |
| Worker/outbox path that flushes shares while the browser is closed | #52 (worker topology) + #6 (engine) — landing |
| Social features themselves (follows, comments, feed, cross-instance lists) | not scheduled; they are the network's product, not the tracker's |
