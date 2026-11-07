<img align="right" width="88" src="https://raw.githubusercontent.com/haaku-tv/.github/main/assets/mark.svg" alt="haaku">

[![Go 1.26+](https://img.shields.io/badge/Go-1.26%2B-00ADD8?style=flat-square&logo=go&logoColor=d8b4fe&labelColor=0d0d0d)](https://go.dev)
[![PostgreSQL 17](https://img.shields.io/badge/PostgreSQL-17-4169E1?style=flat-square&logo=postgresql&logoColor=d8b4fe&labelColor=0d0d0d)](https://www.postgresql.org)
[![License GPLv3](https://img.shields.io/badge/License-GPLv3-c4a5f0?style=flat-square&logo=gnu&logoColor=d8b4fe&labelColor=0d0d0d)](https://github.com/haaku-tv/haaku/blob/main/LICENSE)
[![Status](https://img.shields.io/badge/status-pre--release-c4a5f0?style=flat-square&labelColor=0d0d0d)](https://github.com/haaku-tv/haaku/issues)

[![Stars](https://img.shields.io/github/stars/haaku-tv/haaku?style=flat-square&logo=github&logoColor=d8b4fe&label=Stars&labelColor=0d0d0d&color=c4a5f0)](https://github.com/haaku-tv/haaku/stargazers)
[![Issues](https://img.shields.io/github/issues/haaku-tv/haaku?style=flat-square&logo=github&logoColor=d8b4fe&label=Issues&labelColor=0d0d0d&color=c4a5f0)](https://github.com/haaku-tv/haaku/issues)
[![Last commit](https://img.shields.io/github/last-commit/haaku-tv/haaku?style=flat-square&logo=git&logoColor=d8b4fe&label=Last%20commit&labelColor=0d0d0d&color=c4a5f0)](https://github.com/haaku-tv/haaku/commits)

### haaku

---

> **把握** — *haaku*: “to have a handle on; to keep tabs on a situation.”
> The word Japanese speakers use for the whole picture of where things stand — which is what a tracker does for everything you watch.

---

**haaku** is an open, self-hostable replacement for trakt.tv: watch history, scrobbling, ratings, watchlists, lists, comments and a social feed — on a server you own, in a database you can `psql` into.

This repository is the whole server. The Go API and the React UI compile into **one binary**, and the image that ships it carries **its own PostgreSQL and Redis**, so the install is one `docker compose up -d` and a backup is a directory plus one key.

## What's in it

| | |
|---|---|
| **Watch history** | Scrobble start / pause / stop with progress, plus check-ins for what you watch once |
| **Ratings** | Movies, shows, seasons and individual episodes — episodes rate against the episode, not the show |
| **Lists** | Manual, smart and collaborative lists, alongside collection, watchlist and favourites |
| **Social** | Comments and an activity feed, follows and blocks, notifications, notes and saved filters |
| **Clients** | `tk_…` API keys and OAuth for integrations, outbound webhooks for events |
| **Operations** | Migrations applied on start, a `pg_dump` safety copy before every migration, backup / restore / rotation, secrets encrypted at rest with HKDF-SHA256 → AES-256-GCM, Prometheus metrics on a separate listener |

## Install

### Docker — one container, its own PostgreSQL

> The omnibus image is in review in [#32](https://github.com/haaku-tv/haaku/pull/32). Until it merges, use the source install below.

```bash
git clone https://github.com/haaku-tv/haaku.git
cd haaku
cp .env.example .env       # set TKILLER_SECRET_KEY and TMDB_API_TOKEN
docker compose up -d
```

Then open <http://localhost:8080>. Everything the server owns lives in `./config`: the database, Redis' append-only file, the pre-migration backups and the app's own directory. **Back that directory up together with `TKILLER_SECRET_KEY`** — a dump without the key is unreadable, and the key without the dump is useless.

`docker-compose.advanced.yml` runs the same image against a PostgreSQL and Redis of your own. Setting `DATABASE_URL` / `REDIS_URL` is what switches the bundled services off.

### From source

Go 1.26+, Node 20+ with pnpm, and a PostgreSQL 17. Redis is optional — the code degrades without it.

```bash
cp deploy/.env.example .env      # set TMDB_API_TOKEN and DATABASE_URL
make pg-up                       # throwaway PostgreSQL (unix socket); prints DATABASE_URL
make embed-stub                  # placeholder web/dist so Go builds without a frontend build
make dev-backend                 # API on :8080; migrations run on start
make dev-frontend                # second terminal: Vite on :5173, proxies /v1 to the API
```

The server refuses to start without `TKILLER_SECRET_KEY` (32+ characters — `openssl rand -base64 48`), and a first start on an empty database migrates it for you. `make bin` builds the single binary with the real UI embedded; `make help` lists every target.

## Background workers

Webhook delivery, activity fan-out, the event outbox and the TMDB upkeep jobs run as **background workers**. By default they run inside the API process, so `docker compose up -d` is still the whole install. To run them in a process of their own, set `TKILLER_WORKER_SERVICE=true` in `.env`: the init then starts a second service in the same container and turns the API's own workers off, so each worker runs in exactly one place. `docker-compose.advanced.yml` offers the same split as an opt-in `worker` container (`--profile worker`). From source it is `TKILLER_WORKER=none go run ./cmd/api` for the API and `TKILLER_MODE=worker go run ./cmd/api` for the worker.

The worker process serves `/healthz` and `/readyz` on its own listener and beats a Prometheus heartbeat gauge per worker, so a stalled one is visible. Names, schedule (UTC) and metrics: [docs/architecture/workers.md](docs/architecture/workers.md).

## API

- `/v1` is the versioned surface: JSON in, JSON out, one error envelope — `{"error":{"code":"…","message":"…"}}`.
- Authenticate with `X-API-Key: tk_…` from an integration, or an OAuth bearer token from the UI.
- The spec is **generated from the route registry** and never hand-written: 169 paths / 203 operations in [`openapi.yaml`](openapi.yaml), served live at `GET /v1/openapi.json`. `make api-spec` regenerates it, `make api-check` fails if it is stale or the route inventory drifts, and CI runs both.
- `GET /healthz` for liveness, `GET /readyz` for readiness. Prometheus metrics are on a **separate listener**, deliberately not on the app port.
- The Trakt-shaped surface is coming rather than here: `/v1` is our own shape today, and the compatibility listener plus importer are [#4](https://github.com/haaku-tv/haaku/issues/4) and [#7](https://github.com/haaku-tv/haaku/issues/7).

## Clients

| Client | What it does |
|---|---|
| **web UI** | In this repo, embedded in the binary — no separate deployment, no CORS setup |
| [haaku-jellyfin](https://github.com/haaku-tv/haaku-jellyfin) | Jellyfin plugin: scrobbles playback and syncs watch state to your server |

## Layout

```
cmd/api/            one binary: TKILLER_MODE=api|worker|migrate, or --migrate-only;
                    TKILLER_WORKER picks which background workers a process runs
internal/           ~30 domain packages — handlers, auth, database, cache, workers,
                    obs, backup, secret, mode, webui — each with an audit *.md beside it
internal/webui/     serves the embedded frontend
web/                React + TypeScript frontend, embedded via web/embed.go
migrations/sql/     32 Goose migrations (append-only)
deploy/             compose file and env template for running from source
docs/architecture/  design docs
scripts/            developer tooling (dev-pg.sh boots a throwaway PostgreSQL)
```

## Status

Pre-release, and specific about it. `main` builds, migrates a fresh database and serves, but the pieces that make it a *Trakt replacement* are still landing. **The issues are the source of truth**, not this table.

| Area | State |
|---|---|
| API surface | 169 paths registered, Trakt coverage incomplete — [#4](https://github.com/haaku-tv/haaku/issues/4) |
| Stats, rankings, discovery | Several endpoints answer `501` on purpose until the engines land — [#5](https://github.com/haaku-tv/haaku/issues/5) |
| Sync engine | `POST /v1/syncs` accepts a session that nothing processes yet — [#6](https://github.com/haaku-tv/haaku/issues/6) |
| Trakt importer | Not started; this is what gates moving off trakt.tv — [#7](https://github.com/haaku-tv/haaku/issues/7) |
| Identity | Clerk-only today, so a self-hoster depends on a third-party sign-in service; built-in accounts are an open decision |
| Moderation and retention | Reports are accepted but nothing actions them yet — [#10](https://github.com/haaku-tv/haaku/issues/10) |
| Single image (API + PostgreSQL) | In review — [#32](https://github.com/haaku-tv/haaku/pull/32) |
| CI gates | Lint, vulnerability, secret and coverage gates in review — [#31](https://github.com/haaku-tv/haaku/pull/31) |
| Tracing and alerting | Metrics and worker heartbeats exist; tracing, request-id propagation and alerts do not — [#9](https://github.com/haaku-tv/haaku/issues/9) |

Do not put real data behind this yet.

## Documentation

| Doc | Subject |
|---|---|
| [monorepo.md](docs/architecture/monorepo.md) | Why one repo, one binary, one image |
| [workers.md](docs/architecture/workers.md) | The background workers: names, selector, schedule, metrics |
| [backups.md](docs/architecture/backups.md) | Backups, restore, rotation and the pre-migration safety copy |
| [api-versioning.md](docs/architecture/api-versioning.md) | API versioning, the generated spec and its gates |
| [release-versioning.md](docs/architecture/release-versioning.md) | Release, upgrade and rollback policy |
| [webhooks.md](docs/webhooks.md) | Outbound webhooks and inbound integrations |

Every handler package carries a `*.md` audit next to it — what it does, what it got wrong, what is left. Read the one for the package you touch.

## Contributing

Open an issue before a large change; the open issues are the roadmap. [AGENTS.md](AGENTS.md) is the contract for anyone — human or agent — working here: failing test first, the error envelope everywhere, configuration via environment only, and no re-implementing the cache transaction, advisory-lock or response helpers.

```bash
make test        # Go suites (including _tests_/) + frontend
make lint        # vet + frontend lint
make api-check   # spec freshness and route inventory
```

`openapi.yaml` and `web/src/api/generated/schema.ts` are generated — never hand-edit them; `make api-spec && make api-client` and commit both.

## License

[GNU GPLv3](https://www.gnu.org/licenses/gpl-3.0.html) — see [LICENSE](LICENSE).

---

<p align="center">
  <a href="https://haaku.tv"><img src="https://raw.githubusercontent.com/haaku-tv/.github/main/assets/mark.svg" alt="haaku" width="44" height="44"></a>
  <br><br>
  <sub><b>haaku</b> · keep tabs on everything you watch<br>
  <a href="https://haaku.tv">haaku.tv</a> · <a href="https://github.com/haaku-tv">GitHub</a></sub>
</p>
