#!/usr/bin/env bash
#
# Assert the Goose migration ledger matches the migration files on disk
# (issue #14 section 4): every file has exactly one applied row and every
# applied row has a file.
#
# The expected newest version and row count are DERIVED from migrations/sql at
# the checked-out commit — never hardcoded. This is the same derivation the
# Docker smoke job uses (.github/workflows/docker.yml); a stale constant in one
# place fails the build the first time a migration lands anywhere.
#
# Usage: scripts/check-migration-ledger.sh <DATABASE_URL>
#        (or export DATABASE_URL and omit the argument)
# Environment: MIGRATION_DIR overrides migrations/sql.

set -euo pipefail

dsn="${1:-${DATABASE_URL:-}}"
if [ -z "$dsn" ]; then
  echo "usage: $0 <DATABASE_URL> (or set DATABASE_URL)" >&2
  exit 2
fi

if ! command -v psql >/dev/null 2>&1; then
  echo "check-migration-ledger: psql not found on PATH" >&2
  exit 2
fi

migration_dir="${MIGRATION_DIR:-migrations/sql}"
shopt -s nullglob
files=("$migration_dir"/*.sql)
if [ "${#files[@]}" -eq 0 ]; then
  echo "check-migration-ledger: no *.sql files in $migration_dir" >&2
  exit 1
fi

# Versions from the numeric filename prefix, leading zeros stripped so a legacy
# `0001_x.sql` compares equal to the ledger's `1` (0001_x.sql -> 1), newest last.
expected_versions="$(printf '%s\n' "${files[@]##*/}" \
  | sed -E 's/^0*([0-9]+)_.*/\1/' \
  | sort -n)"
expected_files="$(printf '%s\n' "$expected_versions" | wc -l | tr -d ' ')"
expected_version="$(printf '%s\n' "$expected_versions" | tail -1)"

# Applied versions in the ledger, version 0 (Goose's own marker) excluded.
ledger_versions="$(psql "$dsn" -v ON_ERROR_STOP=1 -tAc \
  'select version_id from goose_db_version where is_applied and version_id <> 0 order by version_id')"
applied="$(printf '%s\n' "$ledger_versions" | grep -c . || true)"
ledger_version="$(printf '%s\n' "$ledger_versions" | tail -1)"

echo "migration ledger: newest=$ledger_version expected=$expected_version applied=$applied files=$expected_files"

status=0
if [ "$ledger_version" != "$expected_version" ]; then
  echo "::error::ledger newest version $ledger_version != newest migration file $expected_version" >&2
  status=1
fi
if [ "$applied" != "$expected_files" ]; then
  echo "::error::ledger has $applied applied rows, want one per file ($expected_files)" >&2
  status=1
fi

# The two counts above can agree while the sets differ (a gap plus a duplicate).
diff="$(diff <(printf '%s\n' "$expected_versions") <(printf '%s\n' "$ledger_versions") || true)"
if [ -n "$diff" ]; then
  echo "::error::ledger versions differ from the files on disk (- only in files, + only in ledger):" >&2
  printf '%s\n' "$diff" >&2
  status=1
fi

if [ "$status" -eq 0 ]; then
  echo "check-migration-ledger: OK — every migration file has exactly one applied ledger row and no row is orphaned"
fi
exit "$status"
