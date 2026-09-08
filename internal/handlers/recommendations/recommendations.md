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
- [x] Fix `full_count` in the combined anticipated branch (count after UNION, not per-CTE). (Batch 12 — the shared union base no longer emits per-part `COUNT(*) OVER()`; the "both" wrapper computes `full_count` over the UNIONed result before ORDER/LIMIT, so the reported total matches the combined item set.)
- [x] Cache `fetchTopGenres` results (Redis, TTL). (Batch 9 — version-keyed per-user with 30m TTL backstop; DB fallback retained.)
- [x] Replace `jsonb_array_elements_text` cross join with a GIN-indexed containment query where possible. (Batch 10 — NOT possible yet: no GIN index exists on `movies.genres`/`shows.genres` (only tsvector GIN on `search_vector`, migration 0003). Cross join kept as-is for correct "any-of-top-genres" results; the index migration + `?|`/`@>` rewrite is documented in the Databases section below and is out of scope here — no schema changes allowed in this batch.)
- [x] Make the score weights / popularity fallback config or spec constants. (Batch 12 — DECISION: keep as in-code constants; the weights are **not spec-defined** and the module has no config pattern, so no config sourcing is added.) `metadata-api.md`'s anticipated route is defined only as "by list activity ('most anticipated')" — it never specifies numeric weights. The current 0.7 (watchlist_count) / 0.3 (popularity) blend and the `popularity DESC` fallback order are module-local implementation choices, embedded in the SQL. The handler module has no config-loading pattern (config is env-only in `cmd/api`; handlers read none), so wiring these through config would be a new pattern with no precedent to follow — higher risk than the value. They remain named by comments in the query and documented here as non-spec constants; if the product later wants tunable weights, that is tracked as a config-pattern task, not guessed into now.)
- [x] Respect `user_hidden` in `handleAnticipated`. (Batch 12 — DECISION: not applicable; `anticipated` is a **site-wide** ranking, not a per-user surface.) `handleAnticipated`'s SQL computes `watchlist_count` by `GROUP BY media_id` over **all** users' watchlists (`user_watchlist` with no user filter) and orders by that global score — it is the global "most anticipated" feed (`/media/anticipated`, metadata-api: "by list activity"), the same class as `/popular`/`/trending`, not a personalized feed. `user_hidden` is a per-user preference (`user_id, section, media_type, media_id`); filtering a site-wide ranking by one viewer's hidden list would both (a) not make sense semantically (the ranking is the same for every requester) and (b) require deciding a hidden `section` scope when the personalized/social paths themselves exclude hidden rows *without* a section qualifier — an ambiguity this global endpoint shouldn't inherit. So `handleAnticipated` correctly ignores `user_hidden`; no code change, decision recorded. Per-user *personalized* anticipated (which would respect hidden) is a separate future surface.)
- [x] Replace error + pagination helpers with `internal/response` (Batch 0).
- [x] Swap local `authRequired` for shared `auth.AuthRequired` (Batch 1).
- [x] `safeStrings`/`safeDateString`/`imageURL` consolidated into `database` package methods (`StringSlice.Values`, `Date.String`, `JSONMap.StringValue`; Batch 4).
- [x] Verify module test coverage (`make test`). (Batch 14 — `internal/handlers/recommendations/recommendations_test.go` covers `topGenresCacheKey` (version+user sensitive). Handler-level integration for the DB-fed personalized/social/anticipated lists awaits a DB-driver approval.)

## Databases — genre containment index (migration needed, out of scope here)

The personalized movies/shows recommendations currently use a
`JOIN jsonb_array_elements_text(genres) AS g ... WHERE g IN (topGenres)` cross
join to match media that has **any** of the user's top genres (OR semantics).
There is **no GIN index** on `movies.genres` or `shows.genres` (`genres` are
`jsonb DEFAULT '[]'`, migration 0002; the only GIN indexes are the tsvector
`search_vector` ones in migration 0003). Without GIN the containment operators
would not be accelerator-usable, so the cross join was **kept as-is** for correct
results — no schema/query change landed in Batch 10 (schema changes are not
allowed in this batch).

When a GIN index migration becomes available (new append-only migration), apply:

```sql
CREATE INDEX idx_movies_genres_gin ON movies USING GIN (genres);
CREATE INDEX idx_shows_genres_gin  ON shows USING GIN (genres);
```

then the two recommendation queries can drop the cross join in favor of the
array-contains operators, preserving the same "any of top genres" semantics:

- movies: replace the `Joins("JOIN jsonb_array_elements_text(movies.genres) AS g ON TRUE")`
  + `Where("g IN ?", topGenres)` with `Where("movies.genres ?| ?", topGenres)`.
- shows: same pattern with `shows.genres ?| ?`.
- The optional per-request `genre` filter already uses the `? ` (string-exists)
  operator, which also benefits from the GIN index.
