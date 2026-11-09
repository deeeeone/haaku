#!/usr/bin/env bash
#
# Size ratchet (issue #8).
#
# tools/size-baseline.txt lists every tracked Go file that is already over the
# soft limit. Those files may only shrink; any other file must stay under the
# limit. The point is not to ban big files — it is to stop the existing ones
# growing while a subsystem is being split up.
#
# Usage:
#   scripts/check-size.sh             # gate (what CI runs)
#   scripts/check-size.sh --update    # rewrite the baseline from the current tree
#
# Env:
#   SOFT_LIMIT  line count above which a file must be tracked (default 800)
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

SOFT_LIMIT="${SOFT_LIMIT:-800}"
BASELINE="tools/size-baseline.txt"

update=0
if [ "${1:-}" = "--update" ]; then
  update=1
fi

current="$(mktemp)"
trap 'rm -f "$current"' EXIT

# Tracked Go files over the limit, largest first, as "<lines>\t<path>".
# Excludes anything under web/ (the frontend has its own tooling).
git ls-files -z '*.go' | while IFS= read -r -d '' f; do
  case "$f" in web/*) continue ;; esac
  [ -f "$f" ] || continue   # staged deletion: still in the index, no lines to count
  n="$(wc -l <"$f")"
  if [ "$n" -gt "$SOFT_LIMIT" ]; then
    printf '%s\t%s\n' "$n" "$f"
  fi
done | sort -rn -k1,1 >"$current"

if [ "$update" = 1 ]; then
  mkdir -p "$(dirname "$BASELINE")"
  {
    printf '# Size ratchet baseline. Format: <lines>\\t<path>\n'
    printf '# These files are already over the %s-line soft limit and may only shrink.\n' "$SOFT_LIMIT"
    printf '# Regenerate deliberately: scripts/check-size.sh --update\n'
    cat "$current"
  } >"$BASELINE"
  echo "wrote $BASELINE:"
  cat "$BASELINE"
  exit 0
fi

if [ ! -f "$BASELINE" ]; then
  echo "FAIL: $BASELINE is missing — run scripts/check-size.sh --update" >&2
  exit 1
fi

failed=0

# 1. Nothing above the limit may be untracked, and nothing tracked may grow.
while IFS=$'\t' read -r n path; do
  [ -n "${path:-}" ] || continue
  recorded="$(awk -F'\t' -v p="$path" '$2 == p { print $1 }' "$BASELINE")"
  if [ -z "$recorded" ]; then
    echo "FAIL: $path is $n lines (> $SOFT_LIMIT) and is not in $BASELINE" >&2
    failed=1
  elif [ "$n" -gt "$recorded" ]; then
    echo "FAIL: $path grew from $recorded to $n lines" >&2
    failed=1
  elif [ "$n" -lt "$recorded" ]; then
    echo "note: $path shrank from $recorded to $n lines — lower the baseline with --update"
  fi
done <"$current"

# 2. Entries that are back under the limit should be removed (not fatal).
while IFS=$'\t' read -r n path; do
  [ -n "${path:-}" ] || continue
  if ! grep -qxF "$path" <(cut -f2 "$current"); then
    echo "note: $path is back under $SOFT_LIMIT lines — remove its baseline entry with --update"
  fi
done <"$BASELINE"

if [ "$failed" != 0 ]; then
  echo "size ratchet failed" >&2
  exit 1
fi

echo "size ratchet ok: no file over $SOFT_LIMIT lines grew ($(wc -l <"$current") tracked)"
