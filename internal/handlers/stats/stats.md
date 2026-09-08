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
| Version-keyed Redis response cache | Done (Batch 9) |

## Oversights

- **Dead code removed: version-keyed Redis cache implemented (RESOLVED)** — the stubbed `cache.GetUserVersion` fetch (`_ = version`) is replaced by a real version-keyed response cache: reads the user's current version, serves a serialized `StatsResponse` for that version directly from Redis, and stores it after a DB-backed compute. A history/data write that bumps the user version invalidates the entry in O(1); misses/outages fall through to the DB table read + recompute (Batch 9).
- **Recompute is uncoordinated** — two concurrent requests that both find stale stats both call `ComputeUserStats`; no advisory lock / singleflight. Wasted work + duplicate writes.
- **Recompute failure is silently swallowed (RESOLVED)** — a failed `ComputeUserStats` (or re-fetch) now returns 503 `db_unavailable` instead of a 200 with zeroed stats.
- **Stats staleness is 1h wall-clock, not version-driven** — user history writes should invalidate instantly (outbox/version counter); instead stale data lives up to an hour.
- **`db.First` error mapping treats ALL errors as 404 (RESOLVED)** — a DB outage now returns 503 `db_unavailable`; only `gorm.ErrRecordNotFound` maps to 404 "user not found".
- **Auth is "optional" in OpenAPI but unused** — the endpoint doesn't read `user_id`; stats are public (fine), but no "extended/self" behavior.
- **Response only exposes aggregate counts** — no breakdown by type/period that the stats engine may compute; verify against spec.
- **Local error helpers** (`writeError`/`badRequest`/`notFound`) duplicated from `internal/httpapi`.
- **`isStale` uses server clock** — untestable without injecting time.

## Todos

- [x] Implement the version-keyed Redis cache path. (Batch 9 — reads user version, serves a serialized `StatsResponse` for that version from Redis, stores after DB compute; version bump invalidates in O(1), DB fallback retained.)
- [x] Guard recompute with an advisory lock or singleflight. (Batch 7 — per-user advisory lock in `recomputeUserStats`)
- [x] Surface recompute failures (503/500) instead of returning zeroed stats. (Batch 6)
- [x] Align staleness with the user version counter (invalidate on writes via outbox). (Batch 7 — version snapshot in Redis; recompute when the counter advances. Residual: implemented locally in the stats module, not wired into the sync write path; the snapshot keys off the user version counter the sync/scrobble cache-tx already bumps, and first-cycle snapshots default to wall-clock until the next compute.)
- [x] Handle non-404 DB errors separately from "not found". (Batch 6)
- [x] Use shared `internal/response` helpers.
- [ ] Verify stats engine output matches the spec'd fields.
- [x] Verify module test coverage (`make test`). (Batch 14 — `internal/handlers/stats/stats_test.go` covers `isStale` (nil/fresh/stale) and `statsVersionSnapshotKey`. Handler-level integration for the stats-engine + Redis compute path awaits a DB-driver approval.)
