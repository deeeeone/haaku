#!/usr/bin/env bash
#
# Boot a throwaway PostgreSQL for development and DB-backed tests.
#
# Docker is not available in every environment (CI runners, agent sandboxes,
# contributor laptops without a daemon), so this uses the host's own server
# binaries when they are installed. It creates a cluster in a temporary
# directory, listens on a unix socket only (no TCP port, so nothing is exposed
# to the network), and prints the DATABASE_URL to export.
#
# Usage:
#   ./scripts/dev-pg.sh start     # create + start, print the URL
#   ./scripts/dev-pg.sh stop      # stop the server (data is kept)
#   ./scripts/dev-pg.sh destroy   # stop and delete the data directory
#   ./scripts/dev-pg.sh status    # report whether it is running
#   eval "$(./scripts/dev-pg.sh start -q)"   # export DATABASE_URL in one line
#
# Environment:
#   HAAKU_PG_DIR   data directory        (default: /tmp/haaku-dev-pg-<checkout id>)
#   HAAKU_PG_SOCK  socket directory      (default: /tmp/haaku-dev-pg-sock-<checkout id>)
#   HAAKU_PG_DB    database name         (default: haaku)
#   HAAKU_PG_USER  role name             (default: haaku)
#   HAAKU_PG_BIN   directory of the postgres binaries (autodetected)
#
# The data directory defaults to a per-checkout path, so two clones or worktrees
# of this repository never share a cluster. Run `./scripts/dev-pg.sh paths` to
# see which ones this checkout uses.

set -euo pipefail

# Default to a cluster PER CHECKOUT.
#
# Two working copies of this repository (a second clone, a git worktree, or two
# agents in one sandbox) must never share a data directory: they would share one
# cluster and one database, and one of them applying a migration, dropping a
# table or seeding rows silently breaks the other's run. The suffix is derived
# from the working copy's path, so the same checkout always reuses its own
# cluster and different checkouts never collide.
#
# Override HAAKU_PG_DIR / HAAKU_PG_SOCK to share one deliberately.
checkout_id() {
  local root
  root="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
  printf '%s' "$root" | cksum | awk '{print $1}'
}

CHECKOUT_ID="$(checkout_id)"

PG_DIR="${HAAKU_PG_DIR:-/tmp/haaku-dev-pg-$CHECKOUT_ID}"
PG_SOCK="${HAAKU_PG_SOCK:-/tmp/haaku-dev-pg-sock-$CHECKOUT_ID}"
PG_DB="${HAAKU_PG_DB:-haaku}"
PG_USER="${HAAKU_PG_USER:-haaku}"
PG_BIN="${HAAKU_PG_BIN:-}"

# Autodetect the newest installed server binaries.
if [ -z "$PG_BIN" ]; then
  PG_BIN="$(find /usr/lib/postgresql -mindepth 2 -maxdepth 2 -type d -name bin 2>/dev/null | sort -V | tail -1 || true)"
fi
if [ -z "$PG_BIN" ]; then
  PG_BIN="$(dirname "$(command -v initdb 2>/dev/null || echo /nonexistent)")"
fi
if [ ! -x "$PG_BIN/pg_ctl" ]; then
  echo "dev-pg: no PostgreSQL server binaries found." >&2
  echo "dev-pg: install them (postgresql-server), or set HAAKU_PG_BIN." >&2
  echo "dev-pg: with Docker, use: make compose-up" >&2
  exit 1
fi

url() { printf 'postgres://%s@/%s?host=%s&sslmode=disable' "$PG_USER" "$PG_DB" "$PG_SOCK"; }

running() { "$PG_BIN/pg_ctl" -D "$PG_DIR" status >/dev/null 2>&1; }

start() {
  local quiet=0
  [ "${1:-}" = "-q" ] && quiet=1

  if running; then
    [ "$quiet" = 0 ] && echo "dev-pg: already running at $PG_DIR" >&2
    url
    return 0
  fi

  mkdir -p "$PG_SOCK"
  if [ ! -s "$PG_DIR/PG_VERSION" ]; then
    echo "dev-pg: creating cluster in $PG_DIR" >&2
    mkdir -p "$PG_DIR"
    "$PG_BIN/initdb" -D "$PG_DIR" -U "$PG_USER" --auth=trust -E UTF8 >/dev/null
  fi

  # listen_addresses='' keeps this on the unix socket only.
  "$PG_BIN/pg_ctl" -D "$PG_DIR" -l "$PG_DIR/server.log" \
    -o "-k $PG_SOCK -c listen_addresses='' -c fsync=off -c full_page_writes=off" \
    start >/dev/null

  # Wait for readiness rather than sleeping a fixed amount.
  for _ in $(seq 1 30); do
    if "$PG_BIN/pg_isready" -h "$PG_SOCK" -U "$PG_USER" >/dev/null 2>&1; then break; fi
    sleep 0.2
  done

  if ! "$PG_BIN/psql" -h "$PG_SOCK" -U "$PG_USER" -d postgres -tAc \
      "SELECT 1 FROM pg_database WHERE datname='$PG_DB'" | grep -q 1; then
    "$PG_BIN/createdb" -h "$PG_SOCK" -U "$PG_USER" "$PG_DB"
    echo "dev-pg: created database $PG_DB" >&2
  fi

  [ "$quiet" = 0 ] && {
    echo "dev-pg: running (socket $PG_SOCK, database $PG_DB)" >&2
    echo "dev-pg: export DATABASE_URL=\"$(url)\"" >&2
  }
  url
}

stop() {
  if running; then
    "$PG_BIN/pg_ctl" -D "$PG_DIR" -m fast stop >/dev/null
    echo "dev-pg: stopped" >&2
  else
    echo "dev-pg: not running" >&2
  fi
}

destroy() {
  stop || true
  rm -rf "$PG_DIR" "$PG_SOCK"
  echo "dev-pg: removed $PG_DIR" >&2
}

status() {
  if running; then
    echo "dev-pg: running at $PG_DIR"
    echo "DATABASE_URL=$(url)"
    "$PG_BIN/psql" -h "$PG_SOCK" -U "$PG_USER" -d "$PG_DB" -tAc 'select version()' 2>/dev/null | head -1 || true
  else
    echo "dev-pg: not running"
    return 1
  fi
}

case "${1:-start}" in
  start)   start "${2:-}" ;;
  stop)    stop ;;
  destroy) destroy ;;
  status)  status ;;
  url)     url ;;
  paths)   echo "HAAKU_PG_DIR=$PG_DIR"; echo "HAAKU_PG_SOCK=$PG_SOCK"; echo "PG_BIN=$PG_BIN" ;;
  *) echo "usage: $0 {start [-q]|stop|destroy|status|url|paths}" >&2; exit 2 ;;
esac
