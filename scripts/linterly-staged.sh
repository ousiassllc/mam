#!/usr/bin/env bash
# pre-commit gate: fail when a file being committed exceeds the linterly line limit
# (.linterly.yml, default 300). `linterly check` takes one path at most, so it runs once over
# the repo and the verdicts are filtered to the files given as arguments. Files that are not
# being committed are never a reason to block, however long they are.
set -euo pipefail

cd "$(dirname "$0")/.."
[[ $# -gt 0 ]] || exit 0
command -v linterly >/dev/null || { echo "linterly not found: go install github.com/ousiassllc/linterly/cmd/linterly@latest" >&2; exit 1; }
command -v jq >/dev/null || { echo "jq not found" >&2; exit 1; }

report="$(mktemp)"
trap 'rm -f "$report"' EXIT
linterly check --no-update-check --format json >"$report" || true # exits 1 while any file anywhere is over the limit; only the staged ones matter here

violations="$(printf '%s\n' "$@" | jq -R . | jq -rs --slurpfile report "$report" '
  . as $staged
  | $report[0].results[]
  | select(.type == "file" and .severity == "error" and (.path as $p | $staged | index($p)))
  | "linterly: \(.path) is \(.lines) lines (limit \(.limit))"')"
if [[ -n "$violations" ]]; then
  echo "$violations" >&2
  exit 1
fi
