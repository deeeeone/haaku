# trakt-killer

The server for **trakt-killer** — an open, community-owned Trakt replacement:
watch history, scrobbling, lists, ratings, social. Feature parity with Trakt
first; importers and a Trakt-compatibility layer follow.

This repository is the whole server: the Go API and the React frontend, compiled
into one binary. See [docs/architecture/monorepo.md](docs/architecture/monorepo.md)
for the layout and why.

## Status

**Pre-release.** The API surface is broad (161 registered endpoints across ~30
handler packages) and the frontend exists, but the pieces that make it
*deployable* are still landing:

| Area | State |
|---|---|
| API endpoints | Broad, but Trakt coverage is incomplete and several endpoints answer `501` until the stats/rankings engine lands |
| Frontend | In-repo and embedded; not yet building in CI |
| Schema migrations | **No runner yet** — the server cannot create its own schema |
| Single image (API + bundled PostgreSQL) | Planned, not built |
| Backups, at-rest secret encryption | Not built |
| Jellyfin client | Separate repository: [`trakt-killer/tkiller-jellyfin`](https://github.com/trakt-killer/tkiller-jellyfin) |

Do not run this in front of real data yet. The tracking issues say what is
missing; they are the source of truth for status, not this table.

## Quick start

Requires Go 1.26+, Node 20+ with pnpm, and a PostgreSQL database.

```bash
cp deploy/.env.example .env        # fill TMDB_API_TOKEN and a DATABASE_URL
make pg-up                         # throwaway PostgreSQL, prints DATABASE_URL
make embed-stub                    # placeholder web/dist so Go builds work
make dev-backend                   # API on :8080
```

For the frontend, in a second terminal:

```bash
make dev-frontend                  # Vite on :5173, proxies /v1 to :8080
```

`make build` produces the binary with the real UI embedded (`make bin`), and
`make help` lists every target.

## Layout

```
cmd/api/            entrypoint (TKILLER_MODE=api|worker|migrate)
internal/           domain packages: handlers, auth, database, cache, workers
internal/webui/     serves the embedded frontend
web/                React + TypeScript frontend (embedded into the binary)
migrations/         SQL migrations
deploy/             compose file and env template for local services
docs/architecture/  design docs
scripts/            developer tooling
```

## Conventions

- Go + Fiber + GORM + PostgreSQL + Redis; TMDB for metadata.
- Error envelope everywhere; code-first OpenAPI generated from the route registry.
- Patterns inherited from theintrodb-api: `WithCacheTransaction`, version-counter
  invalidation, advisory locks, a Redis circuit breaker.
- **Read [AGENTS.md](AGENTS.md)** — it is the contract for all contributors and
  agents working in this repository.
