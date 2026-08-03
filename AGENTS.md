# trakt-api — Agent Guide

Read this before touching anything. This file is the contract for every agent (Hermes, OpenCode, Claude Code, Codex) working in this repo.

## What this is

The API service for **trakt-killer** — an open, community-owned Trakt replacement (watch history, scrobbling, lists, ratings, social). We are building **feature parity with Trakt first**; importers and a Trakt-compat layer are post-MVP.

- **Planning DB (READ ONLY — never edit):** `../trakt-killer-plan/` — specs, schema, patterns, roadmap. The `docs/` paths referenced throughout this file point there.
- **Status:** Phase 0 skeleton. Features land per `../trakt-killer-plan/07-roadmap/roadmap.md` (T0 → T9).

## Stack

- Go 1.26 · Fiber v2 · GORM (Phase 1+) · PostgreSQL · Redis (go-redis) · Clerk (identity, Phase 2) · TMDB (metadata) · S3/MinIO (images)
- Code-first OpenAPI: route registry → generated `openapi.yaml` (decision log 2026-08-02; generator is T0.6)

## Layout

```
cmd/api/            entrypoint (config → middleware → routes → graceful shutdown)
internal/config/    env-only config (Load() once at startup)
internal/httpapi/   Fiber app, middleware, error envelope, route registry
migrations/         SQL migrations (append-only, numbered — T0.4 tooling)
deploy/             docker-compose (postgres, redis, minio, clerk-emulator, mailpit), .env.example
```

Domain packages arrive with the phases: `internal/media`, `internal/sync`, `internal/auth`, `internal/lists`, `internal/social`, `internal/webhooks`, `internal/cache`, `redis/`.

## Commands

```bash
make build          # go build ./...
make test           # go test ./... (TDD required — write failing tests first)
make vet            # go vet ./...
make run            # go run ./cmd/api (needs .env)
make compose-up     # docker compose -f deploy/docker-compose.yml up -d
make compose-down
```

## Conventions (non-negotiable)

1. **Error envelope** — every error response is `{"error":{"code":"...","message":"..."}}` (`internal/httpapi/errors.go`). Status codes per `../trakt-killer-plan/03-api/api-conventions.md` §9. **Never** 420 quota errors or `X-Upgrade-URL` (charter).
2. **Config via env only** — secrets in `.env` (gitignored); never hardcode, never commit. No pass-cli dependency.
3. **DB writes** — use `WithCacheTransaction` (DB commits first, cache invalidations run async), Postgres advisory locks for concurrent writes, version-counter cache invalidation. Patterns: `../trakt-killer-plan/02-architecture/engineering-patterns.md`. Do NOT reimplement these.
4. **TDD** — failing test first, then minimal implementation. `make test` must pass.
5. **Route registry** — every endpoint is registered in `internal/httpapi/routes.go` (later the OpenAPI generator); CI fails on stale generated spec.
6. **Migrations** — append-only, numbered; never edit a merged migration. Schema changes update `../trakt-killer-plan/02-architecture/data-model.md` FIRST.
7. **No caps on personal data** — unlimited history/watchlist/lists. Anti-abuse limits use 429, never paywall semantics.
8. **Identity is Clerk** — dev uses clerk-emulator (`CLERK_API_BASE_URL=http://localhost:4000`, secret `***`); API keys live in Clerk user metadata, looked up by key body, Redis-cached (TIDB pattern). Phase 2 wiring.

## Definition of done

- Tests green (`make test`) + `go vet` clean
- Routes registered; generated OpenAPI fresh (when generator exists)
- Schema doc updated in the planning DB if anything touched the DB
- No secrets, no cross-package edits beyond the task's listed files, no new deps without an issue
- PR < 400 lines

## Forbidden

- Committing to `main` directly (human merges only)
- Editing `../trakt-killer-plan/` specs (propose via issue/PR there)
- Reimplementing cache-tx / advisory locks / version counters
- Inventing endpoints or fields not in the specs — if the spec is ambiguous, ask, don't guess
- Adding 420 / X-Upgrade-URL / VIP-gated behavior (charter)

## Working here

Short-lived branches (`feat/<phase>-<task>`), TDD, self-review, PR → spec-compliance review → code-quality review (different model) → human merge. Full process: `../trakt-killer-plan/07-roadmap/agent-delegation-plan.md`.
