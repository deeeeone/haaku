#!/usr/bin/env bash
#
# Run the postgres-tagged integration suite (issue #14 section 4).
#
# Only the packages that actually carry a `//go:build postgres` file are run.
# The rest of the repository keeps its in-memory sqlite harness and must not be
# dragged onto a real server: many handler/helper tests flip their backend on
# DATABASE_URL and their postgres paths are a separate piece of work (see issue
# #14). A plain `go test -tags postgres ./...` would set DATABASE_URL for the
# whole run and fail on those packages, which is why this scopes the run.
#
# Usage: scripts/test-db.sh [extra go test flags]
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

if [ -z "${DATABASE_URL:-}" ]; then
  echo "test-db: DATABASE_URL is not set — run: make pg-up" >&2
  exit 1
fi

pkgs=()
while IFS= read -r dir; do
  [ -n "$dir" ] && pkgs+=("$dir")
done < <(grep -rl '^//go:build postgres' --include='*.go' . 2>/dev/null | xargs -r -n1 dirname | sort -u)

if [ "${#pkgs[@]}" -eq 0 ]; then
  echo "test-db: no package carries a //go:build postgres file" >&2
  exit 1
fi

echo "test-db: postgres-tagged packages: ${pkgs[*]}"
exec go test -tags postgres "${pkgs[@]}" "$@"
