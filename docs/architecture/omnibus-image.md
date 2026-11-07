# Omnibus image

The image users are meant to run: the app, PostgreSQL and Redis in one
container, supervised by [s6-overlay](https://github.com/just-containers/s6-overlay),
so `docker compose up -d` is the whole install. It is published as
`ghcr.io/haaku-tv/haaku`.

It is not the only supported layout. Setting `DATABASE_URL` or `REDIS_URL`
points the app at a service of its own and leaves the matching bundled one
off — the same image then runs as one container or as one of several
(`docker-compose.advanced.yml`). A `TKILLER_MODE` of `worker` or `migrate`
starts no bundled service and requires both URLs.

By default the bundled install runs the background workers inside the API
process, so one container really is the whole install. Setting
`TKILLER_WORKER_SERVICE=true` moves them to a second service of their own in the
same container (see "Running a worker" below and
[workers.md](workers.md)).

## Two images, one binary

| Image | Source | Contents | Tag |
|---|---|---|---|
| base | root `Dockerfile` (frontend → Go → runtime) | the binary with the embedded UI, plus `postgresql-client` | `<sha>-base` |
| omnibus | `omnibus/Dockerfile` (`FROM ${TKILLER_IMAGE}`) | base + PostgreSQL + Redis + s6-overlay | `<sha>`, `latest`, `build-N` |

`<sha>-base` is a build input, not something to run: nothing moves `latest` to
it. The build adds only what the base does not carry, so both images run the
same binary and the same frontend bundle — a runtime dependency the app needs
belongs in the root `Dockerfile`, or it drifts.

The base image is Debian, not Alpine, because the pre-migration safety backup
runs `pg_dump` from inside the app and that has to be at least the server's
major version; the PGDG apt repository is where a matching client comes from.
`PG_MAJOR` is declared in the base and inherited here so the two cannot
disagree.

The omnibus build uses no layer cache: its runtime half installs PostgreSQL and
Redis from apt, and a cached layer would keep shipping the previous build's
packages instead of the current security updates.

Build locally (the same commands CI runs):

```bash
docker build -t haaku:base .
docker build -f omnibus/Dockerfile --build-arg TKILLER_IMAGE=haaku:base \
  -t haaku:omnibus omnibus/     # context is omnibus/, not the repo root
```

## The s6 service graph

Services live in `omnibus/rootfs/etc/s6-overlay/s6-rc.d`, one directory each,
with a `dependencies.d/` edge to the service it follows. A bundled service that
is off still reports ready and sleeps, so the graph never changes shape.

| Service | Kind | Starts after | Does |
|---|---|---|---|
| `init-envfile` | oneshot | s6's `base` | refuses a start as any uid but 0; turns `FILE__NAME=/path` into `NAME` from a file only root could have written |
| `init-adduser` | oneshot | `init-envfile` | maps the `tkiller` account onto `PUID`/`PGID` |
| `init-config` | oneshot | `init-adduser` | lays out `/config`, checks the settings the binary refuses to start without, decides which bundled services run, exports `DATABASE_URL`/`REDIS_URL` |
| `init-postgres` | oneshot | `init-config` | creates the cluster on first start, refuses a data directory from another major version, writes the image's settings and `pg_hba.conf` |
| `svc-postgres` | longrun | `init-postgres` | runs PostgreSQL on the socket only, holding the `flock` |
| `init-postgres-db` | oneshot | `svc-postgres` | creates the `trakt` database |
| `svc-redis` | longrun | `init-config` | runs Redis with an append-only file |
| `svc-tkiller` | longrun | `init-postgres-db`, `svc-redis` | runs the app as the `tkiller` account |
| `svc-tkiller-worker` | longrun | `init-postgres-db`, `svc-redis` | runs the app as a background worker (`TKILLER_MODE=worker`). Off unless `TKILLER_WORKER_SERVICE=true`, when it reports ready and sleeps |

s6 stops services in reverse order, so the API drains before PostgreSQL's fast
shutdown, and neither `svc-tkiller` nor `svc-tkiller-worker` has a stop timeout
of its own — the container's `stop_grace_period` bounds them, which is why the
Compose files set it to two minutes. `svc-tkiller-worker` is a
notification-based service (`notification-fd` 3): `s6-notifyoncheck` polls its
`/readyz` before it is reported up, the same way `svc-redis` waits for a `PONG`.

The app applies its own migrations at startup (`internal/database.Migrate`), so
there is no schema step here. That is also what keeps the same binary able to
run as a worker that cannot migrate at all.

## Data layout and ownership

```
/config                root        the image's markers only
/config/postgres       postgres    the cluster (999:999)
/config/redis          redis       the append-only file (998:998)
/config/tkiller        tkiller     backups, an optional .env (911:911 by default)
```

The service ids are fixed in `omnibus/Dockerfile`, before the packages are
installed, so `/config/<service>` keeps a valid owner across image updates even
if a later package takes the next free system id. The init scripts run as root
and never write, chown or chmod inside a service directory: a service account
can plant a symlink there and root would follow it. Files in a service
directory are written *as that account* (`write_as`/`append_as` in `lib.sh`).

`/run/postgresql` is the only socket, and the `tkiller` role authenticates by
peer, so **no database password exists**. It is the cluster superuser, which
means it can run programs as the `postgres` account — that is why the bundled
services are started with `exec_clean`, which hands them nothing but `PATH`,
`HOME` and `TZ`. Otherwise `TKILLER_SECRET_KEY` would sit in PostgreSQL's own
environment, readable through the database.

`/config/tkiller` is walked and re-owned only when the recorded ids in
`/config/.tkiller-owner` differ from the account's current ids, or when an entry
in the top two levels is owned wrong. Delete the marker to force a walk.

PostgreSQL holds a `flock(2)` lock on `/config/.postgres.lock` for its whole
life, so a second container started on the same `/config` refuses to start a
second server on the same data directory (`postmaster.pid` only guards servers
in one PID namespace).

## Load-bearing versions

- **`PG_MAJOR`** (17). PostgreSQL refuses a data directory written by another
  major version, and `init-postgres` refuses it too, with instructions. A bump
  means every existing install dumps and restores: treat it as breaking.
- **`S6_OVERLAY_VERSION`** (3.2.3.2), pinned with per-architecture SHA256
  verification. The supervisor is not something to float.

## Configuration

Required, and checked by `init-config` before anything starts, so a
misconfiguration is one line instead of a crash loop:

- `TKILLER_SECRET_KEY` (`openssl rand -base64 48`) — the root key for
  everything stored encrypted at rest. Back it up separately from the database.
- `TMDB_API_TOKEN` — a TMDB v4 read access token.

Useful, and optional: `TKILLER_PORT` (host port; the container always listens on
8080), `TKILLER_CONFIG` (host directory for `/config`), `PUID`/`PGID`, `TZ`,
`DATABASE_URL`, `REDIS_URL`, `APP_ENV` (defaults to `production` here, which also
keeps the dev-only Clerk simulate route from being registered). The background
workers add `TKILLER_WORKER` (which workers a process runs), and the metadata
worker adds `METADATA_SINCE_FILE` and `METADATA_BACKFILL_PAGES`.

`FILE__NAME=/path` sets `NAME` from a file — Docker, Compose and Kubernetes
secrets all work. The path has to resolve, outside `/config`, to a regular file
that no service account owns and that group and others cannot write, in
directories only root can change; anything else is refused with a warning,
because a service account that could rewrite the file could have root export
`TKILLER_SECRET_KEY` under another name.

### Running a worker

The bundled install runs the background workers **inside the API process**, so
`docker compose up -d` is unchanged and one container is still the whole install.
To run them in a process of their own — a crash in a worker then cannot take the
API down, and each has its own memory — set `TKILLER_WORKER_SERVICE=true`:

```bash
# .env
TKILLER_WORKER_SERVICE=true
# optional: only some workers in this service
# TKILLER_WORKER_SERVICE_SELECTION=webhooks,activity,outbox,metadata
```

`init-config` then enables the `svc-tkiller-worker` service and exports
`TKILLER_WORKER=none` for the API, so the API runs API-only and the worker
service owns the workers — each one in exactly one process, never two. The
worker answers `/readyz` on `TKILLER_WORKER_HEALTH_LISTEN` (default
`127.0.0.1:9090`; it cannot be 8080 because the API is already there in this
container), and `s6-notifyoncheck` waits for that before the service counts as
up. `docker compose logs tkiller` shows both processes; look for
`worker service: on` from the init and `starting background workers` from the
worker.

The worker runs as the same `tkiller` account and shares `/config/tkiller`, so
the metadata change-feed cursor and the pre-migration backups stay where they
were. Leaving `TKILLER_WORKER_SERVICE` unset (or `false`) is the default and
starts no worker service. Full details, including the worker names and their
schedule, are in [workers.md](workers.md).

### Not implemented yet

`POSTGRES_TUNE` (pgtune-style sizing, and the restart-only settings a new
cluster should start with) is **not** in this image. Half of it is easy — a
shell script can size `shared_buffers` from the cgroup limit — and half is not:
the `ALTER SYSTEM` side belongs to the app, next to the migrations, so that the
values and their validation live in one place. Until then the bundled cluster
runs PostgreSQL's defaults. Issue #13 tracks it.

## The Compose files

| File | What it runs |
|---|---|
| `docker-compose.yml` | the omnibus install: one service, one `/config` volume, `.env` |
| `docker-compose.advanced.yml` | the same image against a `postgres` and a `redis` service of their own; adds an opt-in `worker` service behind `--profile worker` |
| `dev/compose.yml` | PostgreSQL, Redis, Minio, the Clerk emulator and Mailpit, for running the API and Vite on the host |

`cp .env.example .env`, set the two required values, `docker compose up -d`.

## Upgrading PostgreSQL

A `PG_MAJOR` bump cannot open the old data directory. Dump first, then restore
into the new image:

```bash
docker compose exec tkiller su -s /bin/bash tkiller -c \
  "pg_dump -h /run/postgresql -U tkiller -d trakt -Fc" > trakt.dump
docker compose down
mv config/postgres config/postgres.old      # keep it until the restore works
# pull the new image, start it, and let it create an empty cluster
docker compose up -d
docker compose exec -T tkiller su -s /bin/bash tkiller -c \
  "pg_restore -h /run/postgresql -U tkiller -d trakt --clean --if-exists" < trakt.dump
```

## Starting without root

`/init` needs root to map accounts and hand directories to them. To run this
image as an unprivileged user — the supported way to do it — point the entrypoint
at the binary and give it an external database:

```yaml
services:
  tkiller:
    image: ghcr.io/haaku-tv/haaku:latest
    entrypoint: ["/app/api"]
    user: "911:911"
    environment:
      DATABASE_URL: postgres://...      # required: no bundled PostgreSQL
      TKILLER_SECRET_KEY: ...
      TMDB_API_TOKEN: ...
```

Nothing is created for you in that mode: no `/config` layout, no `PUID` mapping,
no bundled services. The base image is what runs then, and it is the same binary.

## CI

`.github/workflows/docker.yml` builds the base per architecture, pushes a
`<sha>-base` manifest list, builds the omnibus image from it, and publishes
`latest` / `build-N` / `<sha>` (plus a version tag when the ref is one). Its
`smoke` job is the part worth keeping: it installs the image the way the
instructions do, then asserts the acceptance criteria — `/healthz` answers,
the embedded frontend is served, the migration ledger reaches the newest
migration and the schema has its tables, the same image against an external
PostgreSQL starts no bundled server and still migrates it, and SIGTERM exits 0
with PostgreSQL shut down cleanly.

The smoke job covers the default install (workers inside the API). The opt-in
worker service (`TKILLER_WORKER_SERVICE=true`) is not yet exercised there — it
needs its own step, which is a follow-up rather than part of this change.
