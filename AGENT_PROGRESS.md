# Agent Progress — trakt-api

> Last updated: 2026-08-02 · Commit: 81beb02

## Phase 0 — Foundations
- [x] ✅ T0.1 Repo skeleton + Go module + Fiber bootstrap
- [x] ✅ T0.2 docker-compose (postgres, redis, minio, clerk-emulator, mailpit)
- [x] ✅ T0.3 CI skeleton
- [x] ✅ T0.4 Migrations tooling + users table
- [x] ✅ T0.5 Health endpoints + structured logging + error envelope
- [x] ✅ T0.6 Code-first OpenAPI route registry + YAML generator
- [x] ✅ T0.7 Redis layer (keys, circuit breaker, single-flight)
- [x] ✅ T0.8 Cache infra (WithCacheTransaction, version counters)
- [x] ✅ T0.9 App lifecycle (config → middleware → routes → graceful shutdown)

## Phase 1 — Media Core
- [x] ✅ T1.1 TMDB client (search, detail, changes feeds)
- [x] ✅ T1.2 Metadata ingest worker (backfill + sync modes)
- [x] ✅ T1.3 Media models + migrations (movies, shows, seasons, episodes, people, credits, external_ids)
- [x] ✅ T1.4 ID resolver (numeric/slug/tmdb/imdb/tvdb → canonical)
- [x] ✅ T1.5 Media read endpoints (19 routes: movies/shows/seasons/episodes/people)
- [x] ✅ T1.6 Search (full-text + exact + lookup)
- [x] ✅ T1.7 Discovery (trending, popular, recommendations)
- [x] ✅ T1.8 Calendar (upcoming episodes by date range)
- [x] ✅ T1.9 Updates (movies/shows/people changed since date)
- [x] ✅ T1.10 Watch providers (TMDB integration, 7-day cache)

## Phase 2 — Accounts, Auth, OAuth
- [x] ✅ T2.1 Clerk integration + auth middleware (JWKS, token sniffing, OptionalAuth)
- [x] ✅ T2.2 Clerk webhooks receiver (user.created/updated/deleted → DB sync)
- [x] ✅ T2.3 Users model + migration (Clerk ID PK, profile mirror)
- [x] ✅ T2.4 OAuth2 server (PKCE, device flow, client registration, rotating refresh)
- [x] ✅ T2.5 API keys (tk_ in Clerk metadata, Redis cache, generate/list/revoke)
- [x] ✅ T2.6 Privacy middleware (private profiles, blocks stubs)
- [x] ✅ T2.7 Profile endpoints + avatar upload + settings

## Phase 3 — Sync Engine
- [ ] 🟡 T3.1 History (add/remove/get, expansion rules, dedupe)
- [ ] 🟡 T3.2 Collection + watchlist + ratings + favorites
- [ ] 🟡 T3.3 Playback (resume positions) + show-progress reset/undo
- [ ] 🟡 T3.4 Scrobble (start/pause/stop with exact % semantics)
- [ ] 🟡 T3.5 Checkin (create/delete/current, 409 + expires_at)
- [ ] 🟡 T3.6 Transactional outbox (events_outbox → Redis Streams)
- [ ] 🟡 T3.7 Advisory locks (pg_advisory_xact_lock for scrobble/checkin/ratings/reorder)
- [ ] 🟡 T3.8 Stats (per-user plays/minutes/streaks, cached, version-keyed)
- [ ] 🟡 T3.9 Syncs (batch/chunked sync sessions for large libraries)
- [ ] 🟡 T3.10 Hidden items (calendar/progress/recommendations/comments/dropped)

## Phase 4 — Lists & Social
- [ ] ⬜ T4.1–T4.7 (lists, smart lists, collaborations, follows, comments, activity feed, notifications)

## Phase 5 — Importers
- [ ] ⏸️ DEFERRED to post-MVP

## Phases 6–9
- [ ] ⬜ T6.1–T6.8 Web app (SPA)
- [ ] ⬜ T7.1–T7.5 Scrobbling clients (Jellyfin, Emby, Plex)
- [ ] ⬜ T8.1–T8.4 Webhooks & integrations
- [ ] ⬜ T9.1–T9.6 Hardening & launch