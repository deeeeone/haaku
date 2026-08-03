# trakt-api

The API service for **trakt-killer** — an open, community-owned Trakt replacement: watch history, scrobbling, lists, ratings, social. Feature parity first; importers/compat layer post-MVP.

**Status:** Phase 0 skeleton (buildable, health routes, dev stack). Planning DB: `../trakt-killer-plan/` (specs, schema, roadmap).

## Quick start

```bash
cp deploy/.env.example .env   # fill TMDB_API_TOKEN (and compose passwords)
make compose-up               # postgres, redis, minio, clerk-emulator, mailpit
make run                      # API on :8080
curl localhost:8080/healthz   # {"status":"ok"}
curl localhost:8080/v1/nope   # {"error":{"code":"not_found","message":"unknown route"}}
```

## Repo layout

```
cmd/api/          entrypoint (config → middleware → routes → graceful shutdown)
internal/config/  env-only config
internal/httpapi/ Fiber app, error envelope, route registry
migrations/       append-only SQL migrations (tooling: T0.4)
deploy/           docker-compose dev stack + .env.example
```

## Conventions

- Go + Fiber + GORM + Postgres + Redis; Clerk identity (dev = clerk-emulator); TMDB metadata
- Error envelope everywhere; code-first OpenAPI (generated from route registry)
- Patterns inherited from theintrodb-api: `WithCacheTransaction`, version-counter invalidation, advisory locks, Redis circuit breaker
- **Read `AGENTS.md`** — it's the contract for all agents and human contributors.

## Roadmap

Phases T0–T9 in `../trakt-killer-plan/07-roadmap/roadmap.md`. Current focus: Phase 0 foundations (cache infra, OpenAPI generator, migrations) then Phase 1 media core.
