#!/usr/bin/env bash
#
# Trojan Source guard (CVE-2021-42574).
#
# Unicode bidirectional control characters can make source render differently
# from how the compiler reads it, so a human review sees one thing and the build
# sees another. Nothing in this repo needs them: fail the gate and say where.
#
# Usage: scripts/check-bidi.sh
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

# Bidi controls, matched as UTF-8 *bytes* so the check does not depend on the
# locale grep happens to run under (LC_ALL=C below makes \xNN mean a raw byte):
#   U+202A..U+202E embeddings/overrides = e2 80 aa..ae
#   U+2066..U+2069 isolates            = e2 81 a6..a9
#   U+200E/U+200F/U+061C marks         = e2 80 8e / e2 80 8f / d8 9c
PATTERN='(\xe2\x80[\x8e\x8f\xaa\xab\xac\xad\xae]|\xe2\x81[\xa6\xa7\xa8\xa9]|\xd8\x9c)'

failed=0
scanned=0

while IFS= read -r -d '' f; do
  case "$f" in
    *.png | *.jpg | *.jpeg | *.gif | *.ico | *.webp | *.woff | *.woff2 | *.ttf | *.eot | \
      *.pdf | *.zip | *.gz | *.xz | *.wasm | *.so)
      continue
      ;;
  esac
  scanned=$((scanned + 1))
  # -I skips binary files. `-e` (not a bare pattern) so that `--` really does
  # terminate the options: with `-- "$PATTERN" -- "$f"` grep reads the second
  # `--` as a file, errors, and exits 2 even when it matched — a silent pass.
  if hits="$(LC_ALL=C grep -P -I -n -e "$PATTERN" -- "$f" 2>/dev/null)" && [ -n "$hits" ]; then
    echo "bidi control character(s) in $f:" >&2
    printf '%s\n' "$hits" >&2
    failed=1
  fi
done < <(git ls-files -z)

if [ "$failed" != 0 ]; then
  echo "FAIL: bidi control characters found (CVE-2021-42574)" >&2
  exit 1
fi

echo "no bidi control characters in $scanned tracked files"
