# Stats Handler (`internal/handlers/stats`)

## What it does

Single endpoint: `GET /v1/users/:id/stats` — public user statistics (plays, minutes, unique movies/shows/episodes, ratings count). Delegates the actual computation to the `internal/stats` engine (`statsengine.ComputeUserStats` / `GetUserStats`), serves cached stats when fresh (<1h) and recomputes on demand when stale or missing, then bumps the user version counter for cache invalidation.

## Completion

| Piece | Status |
|---|---|
| OpenAPI registration (1 route) | Done |
| User existence check | Done |
| Cached read via `statsengine` | Done |
| Recompute-on-stale (1h) | Done |
| Version-counter bump after recompute | Done |

## Oversights

- **Dead code: Redis version-keyed caching is stubbed** — lines 107-112 fetch `cache.GetUserVersion(userID)` and `_ = version`; the intended "serve version-keyed cached response" path was never implemented. The DB table (`statsengine.GetUserStats`) is the only real cache.
- **Recompute is uncoordinated** — two concurrent requests that both find stale stats both call `ComputeUserStats`; no advisory lock / singleflight. Wasted work + duplicate writes.
- **Recompute failure is silently swallowed** — on `ComputeUserStats` error the handler falls through to whatever exists (possibly nil → zero stats), hiding the error behind a 200 with zeros. Logged, but client can't distinguish "no data" from "computation failed".
- **Stats staleness is 1h wall-clock, not version-driven** — user history writes should invalidate instantly (outbox/version counter); instead stale data lives up to an hour.
- **`db.First` error mapping treats ALL errors as 404** — a DB outage becomes "user not found".
- **Auth is "optional" in OpenAPI but unused** — the endpoint doesn't read `user_id`; stats are public (fine), but no "extended/self" behavior.
- **Response only exposes aggregate counts** — no breakdown by type/period that the stats engine may compute; verify against spec.
- **Local error helpers** (`writeError`/`badRequest`/`notFound`) duplicated from `internal/httpapi`.
- **`isStale` uses server clock** — untestable without injecting time.

## Todos

- [ ] Implement the version-keyed Redis cache path (or remove the stub).
- [ ] Guard recompute with an advisory lock or singleflight.
- [ ] Surface recompute failures (503/500) instead of returning zeroed stats.
- [ ] Align staleness with the user version counter (invalidate on writes via outbox).
- [ ] Handle non-404 DB errors separately from "not found".
- [x] Use shared `internal/response` helpers.
- [ ] Verify stats engine output matches the spec'd fields.
- [ ] Verify module test coverage (`make test`).
