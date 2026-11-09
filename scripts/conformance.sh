#!/usr/bin/env bash
# shellcheck disable=SC2329  # cleanup() is invoked from the EXIT/INT/TERM trap
#
# Boot a seeded, migrated server on a throwaway PostgreSQL and run the Trakt
# conformance harness against it (issue #102).
#
# There is no Docker daemon in every environment this repo is worked in, so the
# database is the host's own server binaries via scripts/dev-pg.sh — the same
# mechanism `make test-db` uses. The cluster is destroyed on exit, whatever the
# harness's result.
#
# Usage:
#   scripts/conformance.sh            # run the harness (exit code is its own)
#   scripts/conformance.sh --keep     # leave the cluster/server up for debugging
#
# Environment:
#   CONFORMANCE_PORT   server port (default 18090; above 18080 by convention)
#   CONFORMANCE_STRICT pass -strict to the harness (fail on a graded op with no case)
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

PORT="${CONFORMANCE_PORT:-18090}"
BASE_URL="http://127.0.0.1:${PORT}"
KEEP=0
[ "${1:-}" = "--keep" ] && KEEP=1

# Build once and run the file, not `go run`: a killed `go run` leaves its
# compiled child alive holding the port.
BIN_DIR="$(mktemp -d)"
API_BIN="$BIN_DIR/api"
HARNESS_BIN="$BIN_DIR/trakt-conformance"
SERVER_PID=""
PG_STARTED=0

# A cluster of our own. dev-pg.sh defaults to a per-checkout data directory, which
# in a developer's worktree is the same cluster "make pg-up" hands them - and this
# script migrates, seeds and then destroys whatever it is pointed at. Taking their
# data with it, or deleting a cluster this run did not create, is not acceptable,
# so use private paths, remember that we created them, and clean up only those.
PG_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/haaku-conformance-pg.XXXXXX")"
export HAAKU_PG_DIR="$PG_ROOT/data"
export HAAKU_PG_SOCK="$PG_ROOT/sock"

cleanup() {
  local status=$?
  if [ -n "$SERVER_PID" ] && kill -0 "$SERVER_PID" 2>/dev/null; then
    kill -TERM "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  if [ "$KEEP" = 1 ]; then
    echo "conformance: --keep, leaving the cluster running in $PG_ROOT" >&2
    echo "conformance: stop it with HAAKU_PG_DIR=\"$HAAKU_PG_DIR\" HAAKU_PG_SOCK=\"$HAAKU_PG_SOCK\" ./scripts/dev-pg.sh destroy" >&2
  elif [ "$PG_STARTED" = 1 ]; then
    ./scripts/dev-pg.sh destroy >/dev/null 2>&1 || true
    rm -rf "$PG_ROOT"
  else
    # Nothing was started, so there is no cluster of ours to destroy - but the
    # empty directory mktemp made is still ours.
    rm -rf "$PG_ROOT"
  fi
  rm -rf "$BIN_DIR"
  exit "$status"
}
trap cleanup EXIT INT TERM

echo "==> go build"
go build -o "$API_BIN" ./cmd/api
go build -o "$HARNESS_BIN" ./cmd/trakt-conformance

echo "==> starting throwaway PostgreSQL"
DATABASE_URL="$(./scripts/dev-pg.sh start -q 2>/dev/null | tail -1)"
[ -n "$DATABASE_URL" ] || { echo "conformance: dev-pg.sh printed no DATABASE_URL" >&2; exit 1; }
PG_STARTED=1
export DATABASE_URL

# The API refuses to start without these; the secret is generated at runtime so
# no secret-shaped literal ever lands in git history (gitleaks scans all of it).
export HAAKU_SECRET_KEY
HAAKU_SECRET_KEY="$(openssl rand -base64 48)"
export TMDB_API_TOKEN="${TMDB_API_TOKEN:-dummy}"
export TMDB_API_KEY="${TMDB_API_KEY:-dummy}"
export HAAKU_SKIP_BACKUP_BEFORE_MIGRATE=true
export HAAKU_AUTH_MODE=local
export HAAKU_ADMIN_EMAIL="owner@conformance.test"
export HAAKU_ADMIN_PASSWORD="conformance-owner-password"
export HAAKU_WORKER=none
export APP_ENV=development
export PORT

echo "==> applying migrations"
"$API_BIN" --migrate-only >/dev/null

echo "==> seeding fixtures"
# The seed writes a local account with a published password; the harness refuses
# without this opt-in, and this script only ever points it at the private,
# throwaway cluster above.
export HAAKU_CONFORMANCE_SEED=1
"$HARNESS_BIN" -seed

echo "==> starting server on $BASE_URL"
"$API_BIN" >"$BIN_DIR/server.log" 2>&1 &
SERVER_PID=$!

ready=0
for _ in $(seq 1 100); do
  if curl -fsS "$BASE_URL/healthz" >/dev/null 2>&1; then
    ready=1
    break
  fi
  if ! kill -0 "$SERVER_PID" 2>/dev/null; then
    echo "conformance: server exited during startup" >&2
    tail -50 "$BIN_DIR/server.log" >&2
    exit 1
  fi
  sleep 0.2
done
if [ "$ready" != 1 ]; then
  echo "conformance: server did not become ready" >&2
  tail -50 "$BIN_DIR/server.log" >&2
  exit 1
fi

args=(-base-url "$BASE_URL")
if [ "${CONFORMANCE_STRICT:-0}" = 1 ]; then
  args+=(-strict)
fi

echo "==> running conformance"
set +e
"$HARNESS_BIN" "${args[@]}"
status=$?
set -e

if [ "$status" -ne 0 ]; then
  echo "==> server log tail (last 80 lines)" >&2
  tail -80 "$BIN_DIR/server.log" >&2
fi
exit "$status"