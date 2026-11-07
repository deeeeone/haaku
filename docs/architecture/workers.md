# Background workers

The work a request must not wait for — delivering outbound webhooks,
fanning scrobble events out to followers' activity feeds, publishing the event
outbox to Redis, and polling TMDB for metadata changes — runs in **background
workers**. This doc is the topology: which workers exist, where they run, how
they are selected, how a deployment keeps two processes from running the same
one, and what a self-hoster does to run them at all.

## One binary, every role

There is one binary (`cmd/api`, the image's `/app/api`). `HAAKU_MODE` picks
the role (see [monorepo.md](monorepo.md)); `HAAKU_WORKER` picks *which workers
that role runs*:

| `HAAKU_MODE` | Serves HTTP | Runs workers | May migrate |
|---|---|---|---|
| `api` (default) | yes | yes (the classic install) | yes |
| `worker` | no | yes | **no** |
| `migrate` | no | no | yes |

A worker never migrates the schema (`internal/mode`), so several workers can run
against one database without one of them changing the tables under the others.

## The workers

| Name | Kind | Cadence | Does |
|---|---|---|---|
| `webhooks` | loop | every ~500 ms | reads the Redis Stream `events` (consumer group `webhooks`) and POSTs each event to the subscribed, active endpoints, with retries, backoff and auto-disable after repeated failures |
| `activity` | loop | every ~500 ms | reads new `events` and writes `activity_feed` rows for the actor's followers |
| `outbox` | loop | every ~1 s | publishes unpublished `events_outbox` rows to the Redis Stream `events` |
| `metadata` | schedule | see below | the TMDB upkeep jobs |
| `stats` | schedule | **no jobs yet** | the seam for the stats/ranking recompute engine (issue #5) |

### Job schedule (UTC)

`metadata` runs two cron jobs, evaluated in UTC (`cron.WithLocation(time.UTC)`):

| Job | Schedule | Does |
|---|---|---|
| change-feed poll | `0 3 * * *` — 03:00 UTC daily | `metadata.RunSync`: refresh movies, shows and people TMDB reports as changed since the cursor in `$METADATA_SINCE_FILE` |
| popular/trending backfill | `0 4 * * 0` — 04:00 UTC, Sundays | `metadata.Backfill`: refill the local media cache from TMDB's popular and trending lists (`$METADATA_BACKFILL_PAGES` pages, default 5) |

Both are idempotent (ingest upserts), so a missed or doubled run costs API
calls, not correctness. This is the logic `cmd/workers/metadata` used to hold;
that binary is retired (nothing built it, and the same code now runs here).

`stats` is **deliberately empty** in this change. The stats and ranking recompute
is a sibling lane (issue #5); giving it a placeholder job here would publish
numbers nobody computed. Selecting `stats` starts the worker, emits one startup
heartbeat, and logs that it has nothing scheduled. The recompute lane fills in
that worker's `Jobs` slice (`internal/workers/catalog.go`, `statsWorker`) and
nothing else here changes.

## The selector

`HAAKU_WORKER` is a comma-separated list of worker names, or:

| Value | Meaning |
|---|---|
| unset / `all` | every worker (the default — an existing install is unchanged) |
| `webhooks,outbox` | only those |
| `none` | no background worker in this process |

An unknown name is an error at startup, not a silent fallthrough: a typo that
started the whole fleet (or none of it) is the wrong kind of wrong. A `worker`
process that selects nothing exits non-zero rather than starting, doing nothing
and looking healthy.

## A worker runs in exactly one process

The workers are not idempotent with respect to each other — two `outbox`
publishers would publish the same row twice, two `activity` readers on the same
stream would fan out the same event twice. The topology makes that a
configuration fact rather than a hope: a process runs **only the workers it
selected** (`workers.Catalog` builds nothing else), so ownership is decided once,
where the process is told what to run.

The API's default is to run *all* of them, because a single-container install is
one process and should stay that way. A deployment that wants the workers in
their own process turns the API's own workers **off** and runs a `worker`
process instead:

- **Omnibus (one container, docker-compose.yml)** — set
  `HAAKU_WORKER_SERVICE=true`. `init-config` then starts the
  `svc-haaku-worker` s6 service and exports `HAAKU_WORKER=none` for the API,
  so the API and the worker service each run one process's worth of workers.
  Optionally restrict the worker service with
  `HAAKU_WORKER_SERVICE_SELECTION=webhooks,metadata`.
- **Multi-container (docker-compose.advanced.yml)** — set `HAAKU_WORKER=none`
  in `.env` (the API stops running them) and bring up the `worker` service:
  `docker compose -f docker-compose.advanced.yml --profile worker up -d`. That
  service reads `HAAKU_WORKER_SELECTION` for its own subset.
- **From source** — `HAAKU_WORKER=none go run ./cmd/api` for the API, and
  `HAAKU_MODE=worker HAAKU_WORKER=all go run ./cmd/api` for the worker.

When the API is only the API it logs `workers: none — API only (this process
runs no background workers)` at startup, so the state is visible in the logs
rather than inferred.

## Readiness, liveness and the heartbeat

A worker-only process serves no API, so it serves its signals on a listener of
its own — `HAAKU_WORKER_HEALTH_LISTEN`, default `127.0.0.1:9090` (a port away
from the API's 8080, because the omnibus worker service shares the container's
network namespace):

| Path | Meaning |
|---|---|
| `GET /healthz` | liveness — the process is up |
| `GET /readyz` | readiness — the loops and the schedule are running (`503` until then) |

Every worker also beats a Prometheus gauge each time it runs
(`obs.NewWorker(name)`), which is what a scrape alerts on:

- `haaku_worker_heartbeat_timestamp_seconds{worker="…"}` — the wall-clock time
  of the last run; **stops advancing** means the worker is dead.
- `haaku_worker_processed_total{worker="…"}` / `haaku_worker_failed_total{worker="…"}`

The `worker` label is the name from the selector, so metrics, logs and
`HAAKU_WORKER` all agree whatever process hosts the worker. The metrics
listener itself is the usual separate one (`HAAKU_METRICS_LISTEN`, off unless
set) — see issue #9.

## Shutdown and failures

- **Graceful shutdown on SIGINT/SIGTERM.** The process's context cancels the
  loops and the schedule; `Runtime.Shutdown` waits for a pass in flight (bounded
  by the caller) before the process exits. The omnibus sets the container's
  `stop_grace_period` accordingly.
- **Panic recovery per job.** A panic in a loop pass or a scheduled job is
  caught, counted as a failure and logged with a stack; the loop or schedule
  carries on. One bad pass does not take the process down.
- **Skipped work is retried.** A pass that finds no database or Redis (a
  not-yet-ready dependency) returns without error and the next pass retries;
  workers never treat a missing dependency as fatal at startup.

## Metrics to alert on

```promql
# A worker that has not beaten in 5 minutes.
time() - haaku_worker_heartbeat_timestamp_seconds > 300
```

## Not here yet

- **The stats/ranking recompute (issue #5).** The `stats` worker is the seam;
  its job list is empty on purpose.
- **A sync-session worker (issue #6).** `POST /v1/syncs` accepts a session that
  nothing processes yet. When the engine lands it registers as one more worker
  here — a name in `internal/workers/catalog.go` and, if it is long-running, a
  line in the docs table above. This change is the deployment half: the topology
  it runs in.
