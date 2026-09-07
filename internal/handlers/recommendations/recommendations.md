# Recommendations Handler (`internal/handlers/recommendations`)

## What it does

Five read-only endpoints, all auth-required:

- `GET /v1/recommendations/movies` & `/shows` — **personalized**: computes the user's top 5 genres (UNION of history + favorites + ratings across movies/shows), then returns unwatched, un-hidden movies/shows in those genres, ordered `popularity DESC`. Optional `genre` filter; paginated.
- `GET /v1/social_recommendations/movies` & `/shows` — **social**: mutual friends (approved bidirectional follows) with watches in the last 90 days; returns unwatched-by-you titles ordered `friend_count DESC, popularity DESC`, with `friend_count`.
- `GET /v1/media/anticipated` — **anticipated**: shows with unaired episodes + movies releasing within [now−30d, now+90d], scored `watchlist_count*0.7 + popularity*0.3`, optional `type=movies|shows` filter, paginated.

This is the **only** recommendations implementation (the `discovery` module's recs are stubs). Registered with OpenAPI.

## Completion

| Piece | Status |
|---|---|
| OpenAPI registration (5 routes) | Done |
| Personalized movies/shows (top-genres + unwatched) | Done |
| Social movies/shows (mutual friends, 90d) | Done |
| Anticipated media (weighted score, type filter) | Done |
| Pagination + genre/type filters | Done |

## Oversights

- **`requireAuth` was middleware-less (RESOLVED)** — routes were registered WITHOUT `requireAuth`; only the per-handler `c.Locals("user_id")` check guarded them (recommendations.go:60-70). Handlers already fail closed (checked asserts, 401), so behavior is unchanged — but all 5 routes now run `auth.AuthRequired()` at registration, matching the OpenAPI Auth:required annotation and enforcing auth before any DB work.
- **Anticipated `watchlist_count`/`score` ordering** — in the combined branch (`mediaType` empty) the inner CTEs each compute `COUNT(*) OVER()` before `UNION ALL`, so `full_count` is the per-type count, not the combined total; the outer wrapper re-orders by `score` but the reported `row_count` is misleading when both types are returned.
- **`fetchTopGenres` now cached (RESOLVED)** — the per-user top-genres query is cached in Redis, version-keyed by the user's version counter (a history/favorites/ratings write that bumps it invalidates in O(1)), with a 30m TTL backstop. Best-effort, DB fallback (Batch 9).
- **`jsonb_array_elements_text` cross join** — `JOIN jsonb_array_elements_text(movies.genres) AS g ON TRUE` on the movies table; combined with `DISTINCT` this is functional but expensive (full-table expansion before filtering). No index-friendly path.
- **`Date.String()` (nil) returns `""`** — shared `database.Date.String()` is nil-safe; combined with `released,omitempty` semantics it's fine, yet `first_aired`/`release_date` zero-dates silently become empty strings.
- **Cutoff is `time.Now()` (server clock)** — 90-day window is per-request; fine for a wall-clock read but untestable; no injection of `now()`.
- **Local helpers (RESOLVED)** — `parsePagination` (shared), `authRequired` (shared middleware), `internalError` (response helpers), and `safeStrings`/`safeDateString`/`imageURL` (now `database.StringSlice.Values`, `database.Date.String`, `database.JSONMap.StringValue`; Batch 4).
- **Hardcoded score weights** (0.7/0.3) and `popularity DESC` fallback — not spec-driven constants.
- **No `extended`/limit validation against spec** — `limit` clamped to 100 but the API convention may cap differently (check `03-api/api-conventions.md`).
- **No exclusion of already-anticipated hidden items in `handleAnticipated`** — personalization/social exclude `user_hidden`, but anticipated does not.

## Todos

- [x] Attach `auth.AuthRequired` to route registration (all 5 routes).
- [ ] Fix `full_count` in the combined anticipated branch (count after UNION, not per-CTE).
- [x] Cache `fetchTopGenres` results (Redis, TTL). (Batch 9 — version-keyed per-user with 30m TTL backstop; DB fallback retained.)
- [ ] Replace `jsonb_array_elements_text` cross join with a GIN-indexed containment query where possible.
- [ ] Make the score weights / popularity fallback config or spec constants.
- [ ] Respect `user_hidden` in `handleAnticipated`.
- [x] Replace error + pagination helpers with `internal/response` (Batch 0).
- [x] Swap local `authRequired` for shared `auth.AuthRequired` (Batch 1).
- [x] `safeStrings`/`safeDateString`/`imageURL` consolidated into `database` package methods (`StringSlice.Values`, `Date.String`, `JSONMap.StringValue`; Batch 4).
- [ ] Verify module test coverage (`make test`).
