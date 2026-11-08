# internal/ratelimit — the anti-abuse limiter

Two ceilings, both in Redis, both per endpoint group and per identity:

| Layer | Shape | Window | Refusal |
|---|---|---|---|
| Burst | sorted set, sliding window (ZADD / ZREMRANGEBYSCORE / ZCARD) | `RATE_LIMIT_WINDOW_SECONDS`, default 60s | `429 rate_limit_exceeded` |
| Budget | counter, `INCR` on a day-suffixed key | the instance's local day | `429 usage_limit_exceeded` |

Installed once, as a global middleware (`internal/httpapi/server.go`, step 5),
**before** the routes. It is not per-route: the group is derived from the method
and the path (`GroupFor`), so adding an endpoint cannot accidentally skip the
limiter.

## Groups

A group is a counter namespace, not a route group. Two surfaces in the same
group share an allowance; two surfaces in different groups can never spend each
other's.

| Group | Paths (first segment, `/v1` stripped) | Why it is separate |
|---|---|---|
| `sync` | `sync/*`, `syncs/*`, `scrobble/*` | A library scan is the one workload that legitimately spends thousands of calls back to back. Throttling it at browsing pace would make the Jellyfin plugin unusable. |
| `media` | `movies`, `shows`, `seasons`, `episodes`, `people`, `search`, `calendars`, `updates`, `reference`, `watch`, `media`, `networks`, `languages`, `genres`, `countries`, `certifications`, `recommendations` | Expensive TMDB-backed reads, and `/v1/search` is the most expensive query in the API (issue #10 §5). A media page load must not spend the feed's allowance. |
| `write` | any other non-GET/HEAD | Comment, follow, list and webhook spam. |
| `auth` | `auth`, `oauth`, `webhooks` | Login has its own, stricter limiter (internal/auth: per IP and per hashed address). This group only stops a loop. |
| `admin` | `admin` | The operator surface. |
| `read` | everything else | The steady state of the web UI. |

Ceilings live in `groups.go` (`groupTiers`) as a table, not as sixteen env vars:
the numbers belong to the design, and a table with one shared window knob is
reviewed in a diff. `0` for a daily cell means "no budget for this tier" — how a
surface that requires authentication says so; the burst ceiling still applies.

Nothing here caps **data**. History, lists, watchlist and collection are
unbounded by charter (AGENTS.md §7). These ceilings cap request *volume*.

## Identity

* Signed in → the account id. Anonymous → the client IP.
* The two tiers have different keyspaces as well as different ceilings, so one
  can never spend the other's allowance.
* `ClientIP` believes `X-Forwarded-For` / `X-Real-IP` **only** when the TCP peer
  is loopback or in `TRUSTED_PROXY_CIDRS`; then it takes the rightmost entry that
  is not itself trusted (a proxy appends the address it saw). Anything to the
  left of that was supplied by the client and is worth nothing. An untrusted peer
  is metered at its own address, headers ignored.
* A loopback peer with **no** forwarded header is this machine — the operator's
  shell, the container's health check, a dev server that forwards without
  headers — and is exempt when `RATE_LIMIT_EXEMPT_LOCAL=true` (the default;
  TheIntroDB exempts loopback the same way). The same peer **with** a forwarded
  header is a real client and is metered.
* `ADMIN_USER_IDS` / `MOD_USER_IDS` are never throttled. This mirrors
  TheIntroDB's admin/mod whitelist, and it is the allowlist itself (in-memory)
  rather than `users.is_admin`: that would add a `users` read to every request's
  hot path.

## Response contract

Every limited response carries both allowances, so a client can back off before
it is refused:

```
X-RateLimit-Limit / -Remaining / -Reset      burst window
X-UsageLimit-Limit / -Remaining / -Reset      daily budget
Retry-After                                   set on either 429, delta-seconds
```

A refusal is the standard envelope (AGENTS.md §1) with a distinguishing code:

```json
{"error":{"code":"rate_limit_exceeded","message":"too many requests — slow down"}}
{"error":{"code":"usage_limit_exceeded","message":"daily request budget for this endpoint group is exhausted — try again tomorrow"}}
```

The two codes exist because "slow down for a few seconds" and "you are out of
budget until tomorrow" need different client behaviour. `Retry-After` is never
`0`: a client that reads zero retries immediately.

`/healthz`, `/readyz`, `/metrics`, `/v1/openapi.json` and every `OPTIONS` request
skip the limiter. Refusing a supervisor's health check makes an instance look
down exactly when it is busy.

## Failure mode

Redis unavailable → both layers FAIL OPEN, with a WARN line
(`rate limiter unavailable — failing open`). A limiter that fails closed turns a
Redis blip into a total API outage, which on a self-hosted box is a worse failure
than the abuse it prevents. TheIntroDB chose the opposite for its usage layer;
that service is multi-tenant and publicly reachable, where abuse is the larger
risk.

## Log lines

```
WARN rate limited layer=burst identifier=local_abc group=read count=61 max=60 path=/v1/users/me/activity
WARN rate limited layer=budget identifier=198.51.100.7 group=write count=10001 max=10000 path=/v1/comments
```

`layer` names which ceiling refused, `count`/`max` the actual numbers, and
`identifier` the account or IP — so one `grep 'level=WARN msg="rate limited"'`
answers "who is hammering what". `haaku_http_rate_limited_total` counts both
layers (internal/obs).

## Limitations

* **The counters are shared by every replica** — both layers are Redis, so N
  replicas meter one shared allowance. The corollary is that two replicas pointed
  at two *different* Redis instances each meter half the traffic.
* **A proxy that does not send forwarded headers makes every client look like
  the proxy.** Then all anonymous traffic shares one bucket. Fix it at the proxy
  (`TRUSTED_PROXY_CIDRS` plus the proxy's own `X-Forwarded-For`), or leave
  `RATE_LIMIT_EXEMPT_LOCAL=true` on a loopback proxy and accept that loopback
  traffic is unmetered.
* **A shared NAT or a proxy under someone else's control collapses many users
  into one bucket.** Only relevant to the hosted instance; a household is one
  bucket per member.
* **No per-target ceiling.** TheIntroDB also limits repeated calls to one exact
  media target; haaku does not. A client hammering one media id spends its
  group's burst and budget, which is enough for one household, but a hosted
  instance with many users behind one target would want the extra counter.
* **The daily budget resets at the instance's local midnight** (the container's
  `TZ`), not UTC and not per user.
* **Invalid credentials are charged as anonymous.** A request with a bad token is
  metered by IP in the public tier; the 401 it receives still consumes an
  allowance.
* **Two Redis round trips per limited request** (burst, then budget). Both are
  pipelines of a few commands; they are not merged into one `EVAL` because the
  burst gates the budget (a request the burst refused is not charged to the
  day).
