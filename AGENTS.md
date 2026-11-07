# haaku — Agent Guide

Read this before touching anything. This file is the contract for every agent (Hermes, OpenCode, Claude Code, Codex) working in this repo.

## What this is

The server for **haaku** — an open, self-hostable Trakt replacement (watch history, scrobbling, lists, ratings, social). We are building **feature parity with Trakt first**; importers and a Trakt-compat layer are post-MVP. The Go module is still named `haaku` and environment variables are still `HAAKU_*`; the product is haaku (`haaku.tv`).

- **Planning DB (READ ONLY — never edit):** `../haaku-plan/` — specs, schema, patterns, roadmap. The `docs/` paths referenced throughout this file point there.
- **Status:** pre-release. The API surface is broad; the deployable pieces (schema migrations, the single image, backups) are still landing. The repository issues are the source of truth for status. See `docs/architecture/monorepo.md` for the layout.

## Stack

- Go 1.26 · Fiber v2 · GORM (Phase 1+) · PostgreSQL · Redis (go-redis) · Clerk (identity, Phase 2) · TMDB (metadata) · S3/MinIO (images)
- Code-first OpenAPI: route registry → generated `openapi.yaml` (decision log 2026-08-02; generator is T0.6)

## Layout

```
cmd/api/            entrypoint (config → mode → middleware → routes → graceful shutdown)
internal/config/    env-only config (Load() once at startup)
internal/httpapi/   Fiber app, middleware, error envelope, route registry
internal/webui/     serves the embedded frontend (web/dist)
internal/mode/      run modes (api | worker | migrate)
web/                React frontend, embedded into the binary (web/embed.go)
migrations/         SQL migrations (append-only, numbered — T0.4 tooling)
Dockerfile          the base image: frontend + binary + postgresql-client
omnibus/            the single image: + PostgreSQL, Redis, s6-overlay (issue #13)
docker-compose.yml  the omnibus install (one container, one command)
dev/compose.yml     PostgreSQL, Redis, Minio, Clerk emulator, Mailpit for host dev
deploy/.env.example the longer env list for running the API from source
docs/architecture/  design docs for decisions that outgrow a comment
scripts/            developer tooling (dev-pg.sh boots a throwaway PostgreSQL)
```

Domain packages arrive with the phases: `internal/media`, `internal/sync`, `internal/auth`, `internal/lists`, `internal/social`, `internal/webhooks`, `internal/cache`, `redis/`.

## Commands

```bash
make help           # list every target
make build          # embed-stub + go build ./...
make test           # Go suites (test-go) + frontend (test-web)
make test-go        # both Go suites: ./... AND the _tests_ harness (one runner)
make vet            # go vet ./...
make lint-web       # frontend gate: biome format check + biome ci (warnings fatal)
make check          # every gate CI runs (lint, vuln, secrets, hygiene, spec)
make coverage       # both Go suites under one profile + the coverage floor
make bin            # frontend + the real single binary (bin/api)
make dev-backend    # go run ./cmd/api (needs .env)
make dev-frontend   # Vite dev server
make pg-up          # throwaway PostgreSQL; prints DATABASE_URL
make compose-up     # dev infrastructure: postgres, redis, minio, clerk-emulator, mailpit
make docker-omnibus # build the single image (app + PostgreSQL + Redis, s6-overlay)
```

### Two things that will bite you

1. **`web/dist` must exist for any Go build.** `web/embed.go` embeds it. `make build` and `make test` depend on `embed-stub`, which writes a placeholder; a bare `go build ./...` on a fresh checkout fails until you run it once.
2. **Modes.** `HAAKU_MODE=api|worker|migrate` (or `--migrate-only`) selects the role. Only `api` and `migrate` may change the schema; a worker never does.
3. **`_tests_/` is invisible to `./...`.** A leading underscore hides a directory from the pattern, so `go test ./...` and `golangci-lint run ./...` both skip it. `make test-go` and `make lint-go` run it as a second, explicit invocation — see `docs/architecture/ci-gates.md`.

## Conventions (non-negotiable)

1. **Error envelope** — every error response is `{"error":{"code":"...","message":"..."}}` (`internal/httpapi/errors.go`). Status codes per `../haaku-plan/03-api/api-conventions.md` §9. **Never** 420 quota errors or `X-Upgrade-URL` (charter).
2. **Config via env only** — secrets in `.env` (gitignored); never hardcode, never commit. No pass-cli dependency.
3. **DB writes** — use `WithCacheTransaction` (DB commits first, cache invalidations run async), Postgres advisory locks for concurrent writes, version-counter cache invalidation. Patterns: `../haaku-plan/02-architecture/engineering-patterns.md`. Do NOT reimplement these.
4. **TDD** — failing test first, then minimal implementation. `make test` must pass.
5. **Route registry** — every endpoint is registered in `internal/httpapi/routes.go` (later the OpenAPI generator); CI fails on stale generated spec.
6. **Migrations** — append-only, numbered; never edit a merged migration. Schema changes update `../haaku-plan/02-architecture/data-model.md` FIRST.
7. **No caps on personal data** — unlimited history/watchlist/lists. Anti-abuse limits use 429, never paywall semantics.
8. **Identity is Clerk** — dev uses clerk-emulator (`CLERK_API_BASE_URL=http://localhost:4000`, secret `***`); API keys live in Clerk user metadata, looked up by key body, Redis-cached (TIDB pattern). Phase 2 wiring.

## Definition of done

- Tests green (`make test`) + `go vet` clean
- Gates green (`make check`: golangci-lint, govulncheck, gitleaks, actionlint, shellcheck, size ratchet, bidi check, spec freshness)
- Frontend gate green (`make lint-web` + `make test-web`): biome format check and strict lint (warnings fatal); `make check` stays frontend-free — it runs on a host with no node toolchain
- Routes registered; generated OpenAPI fresh (when generator exists)
- Schema doc updated in the planning DB if anything touched the DB
- No secrets, no cross-package edits beyond the task's listed files, no new deps without an issue
- PR < 400 lines

## Forbidden

- Committing to `main` directly (human merges only)
- Editing `../haaku-plan/` specs (propose via issue/PR there)
- Reimplementing cache-tx / advisory locks / version counters
- Inventing endpoints or fields not in the specs — if the spec is ambiguous, ask, don't guess
- Adding 420 / X-Upgrade-URL / VIP-gated behavior (charter)

## Working here

Short-lived branches (`feat/<phase>-<task>`), TDD, self-review, PR → spec-compliance review → code-quality review (different model) → human merge. Full process: `../haaku-plan/07-roadmap/agent-delegation-plan.md`.
