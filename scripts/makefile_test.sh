#!/usr/bin/env bash
# Checks the Makefile: every command target is in .PHONY and is listed (with a `##` description) by `make help`.
# Run: bash scripts/makefile_test.sh
set -euo pipefail

cd "$(dirname "$0")/.."
# Command targets: plain `name:` lines. File targets (static/node_modules) contain a `/`, so the pattern skips them.
targets="$(grep -oE '^[a-z][a-z-]*:' Makefile | tr -d ':' | sort -u)"
# `make -q` exits non-zero when it would rebuild; only its database dump is wanted.
phony="$({ make -pRrq : 2>/dev/null || true; } | sed -n 's/^\.PHONY: *//p' | tr ' ' '\n' | sort -u)"
help="$(make --no-print-directory help | awk '{print $1}' | sort -u)"

fails=0
for t in $targets; do
  grep -qx -- "$t" <<<"$phony" || { echo "FAIL: '$t' is not in .PHONY"; fails=$((fails + 1)); }
  grep -qx -- "$t" <<<"$help" || { echo "FAIL: '$t' is not listed by 'make help' (add a '## description')"; fails=$((fails + 1)); }
done
if ((fails > 0)); then
  exit 1
fi
echo "ok: every Makefile target is in .PHONY and listed by make help"
