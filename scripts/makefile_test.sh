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

# Code generation must use the templ version pinned in go.mod, not whatever `templ` is on PATH (a stale binary
# writes *_templ.go that CI's freshness check rejects).
templ_version="$(go list -m -f '{{.Version}}' github.com/a-h/templ)"
generate_cmd="$(make --no-print-directory -n generate)"
expected="go run github.com/a-h/templ/cmd/templ@${templ_version} generate ./backend/..."
if [[ "$generate_cmd" != "$expected" ]]; then
  echo "FAIL: 'make generate' runs '$generate_cmd', want '$expected'"
  exit 1
fi
echo "ok: make generate runs templ ${templ_version} (go.mod), not the templ on PATH"

# The distributed Windows build (cmd/wails, //go:build windows) is invisible to a Linux govulncheck run, so `make vuln`
# must check the Windows target as well.
vuln_cmd="$(make --no-print-directory -n vuln)"
for want in 'govulncheck ./...' 'GOOS=windows CGO_ENABLED=0 govulncheck ./...'; do
  grep -qxF -- "$want" <<<"$vuln_cmd" || { echo "FAIL: 'make vuln' does not run '$want'"; exit 1; }
done
echo "ok: make vuln checks both the Linux host and the Windows target"

# scripts/package-windows.sh (Windows has no make) must pin templ the same way: a PATH `templ generate` would rewrite the
# committed *_templ.go with a stale version, and `templ` must not be a required tool any more.
if grep -qE '^[[:space:]]*templ generate' scripts/package-windows.sh || grep -qE 'for tool in .*\btempl\b' scripts/package-windows.sh; then
  echo "FAIL: scripts/package-windows.sh uses the templ on PATH; use 'go run github.com/a-h/templ/cmd/templ@<go.mod version> generate'"
  exit 1
fi
# shellcheck disable=SC2016 # the literal $templ_version in package-windows.sh is what is searched for
grep -qF 'go run github.com/a-h/templ/cmd/templ@"$templ_version" generate ./backend/...' scripts/package-windows.sh || {
  echo "FAIL: scripts/package-windows.sh does not run templ at the go.mod version"
  exit 1
}
echo "ok: scripts/package-windows.sh runs templ ${templ_version} (go.mod), not the templ on PATH"
