#!/usr/bin/env bash
# Tests for scripts/check-third-party-notices.sh with a stubbed generator (the real one needs go, node and the npm packages).
# Run: bash scripts/check-third-party-notices_test.sh
set -euo pipefail

script="$(cd "$(dirname "$0")" && pwd)/check-third-party-notices.sh"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/repo/scripts"
cp "$script" "$work/repo/scripts/"
# The stub generator writes $FAKE_NOTICES to the file named by its first argument.
cat >"$work/repo/scripts/third-party-notices.sh" <<'STUB'
#!/usr/bin/env bash
printf '%s' "$FAKE_NOTICES" >"$1"
STUB
printf 'notices v1\n' >"$work/repo/THIRD_PARTY_NOTICES.md"

fails=0
check() { # <name> <want exit code> <FAKE_NOTICES>
  local code=0
  (cd "$work/repo" && FAKE_NOTICES="$3" bash scripts/check-third-party-notices.sh >/dev/null 2>&1) || code=$?
  if [[ "$code" == "$2" ]]; then echo "ok: $1"; else echo "FAIL: $1 (exit $code, want $2)"; fails=$((fails + 1)); fi
}
check "the committed file is what the generator writes" 0 $'notices v1\n'
check "a file the generator would write differently fails" 1 $'notices v2\n'
check "a missing final newline fails" 1 'notices v1'
exit $((fails > 0))
