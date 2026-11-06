#!/usr/bin/env bash
#
# One Go test runner (issue #8).
#
# `go test ./...` cannot see `_tests_/`: a leading underscore makes a directory
# invisible to the `./...` pattern, so the integration harness was silently
# skipped by the obvious command. This always runs both suites, and can emit a
# single merged coverage profile for both of them.
#
# Usage:
#   scripts/test-go.sh                        # both suites
#   scripts/test-go.sh -run TestSomething     # extra flags go to both suites
#   COVERAGE_OUT=coverage.out scripts/test-go.sh
#   COVERAGE_OUT=coverage.out COVERAGE_FLOOR=22 scripts/test-go.sh
#
# Env:
#   COVERAGE_OUT    write a merged coverage profile here (implies -coverpkg=./...)
#   COVERAGE_FLOOR  fail if the merged total is below this percentage (0 = off)
#
# `-coverpkg=./...` is required for the merged profile to mean anything: without
# it each run only instruments the package under test, and the harness (whose own
# files are test code) would contribute nothing.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

COVERAGE_OUT="${COVERAGE_OUT:-}"
COVERAGE_FLOOR="${COVERAGE_FLOOR:-0}"

# Extra args are passed through to both `go test` invocations.
extra=("$@")

work=""
if [ -n "$COVERAGE_OUT" ]; then
  work="$(mktemp -d)"
  trap 'rm -rf "$work"' EXIT
fi

run_suite() {
  local name="$1" profile="$2"
  shift 2
  echo "==> go test ${name}"
  if [ -n "$profile" ]; then
    go test -covermode=set -coverpkg=./... -coverprofile="$profile" ${extra[@]+"${extra[@]}"} "$@"
  else
    go test ${extra[@]+"${extra[@]}"} "$@"
  fi
}

run_suite "unit (./...)" "${work:+$work/unit.out}" ./...
run_suite "harness (_tests_)" "${work:+$work/harness.out}" ./_tests_/*.go

if [ -n "$COVERAGE_OUT" ]; then
  # Both profiles are `mode: set`; keep one header and concatenate the blocks.
  { head -1 "$work/unit.out"; tail -n +2 "$work/unit.out"; tail -n +2 "$work/harness.out"; } > "$COVERAGE_OUT"
  total="$(go tool cover -func="$COVERAGE_OUT" | awk '/^total:/ {gsub(/%/, "", $NF); print $NF}')"
  echo "==> total coverage: ${total}%  (profile: ${COVERAGE_OUT})"
  if [ "$COVERAGE_FLOOR" != "0" ] && awk -v t="$total" -v f="$COVERAGE_FLOOR" 'BEGIN { exit !(t + 0 < f + 0) }'; then
    echo "FAIL: coverage ${total}% is below the floor of ${COVERAGE_FLOOR}%" >&2
    exit 1
  fi
fi
