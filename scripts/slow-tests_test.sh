#!/usr/bin/env bash
# Tests for scripts/slow-tests.sh with canned `go test -json` streams.
# Run: bash scripts/slow-tests_test.sh
set -euo pipefail

script="$(cd "$(dirname "$0")" && pwd)/slow-tests.sh"
fails=0

stream() {
  cat <<'JSON'
{"Action":"output","Package":"x/backend/internal/router","Test":"TestFast","Output":"fast says hi\n"}
{"Action":"pass","Package":"x/backend/internal/router","Test":"TestFast","Elapsed":0.2}
{"Action":"pass","Package":"x/backend/internal/router","Test":"TestMedium","Elapsed":4.5}
{"Action":"pass","Package":"x/backend/internal/router","Test":"TestSlowest","Elapsed":9.25}
{"Action":"pass","Package":"x/backend/internal/router","Elapsed":30}
build failed: plain text
JSON
}

# check <name> <command...>: ok when the command succeeds.
check() {
  local name="$1"
  shift
  if "$@"; then echo "ok: $name"; else echo "FAIL: $name"; fails=$((fails + 1)); fi
}
has() { [[ "$1" == *"$2"* ]]; }
lacks() { [[ "$1" != *"$2"* ]]; }
# before <text> <a> <b>: a appears before b.
before() { local head="${1%%"$3"*}"; [[ "$1" == *"$3"* && "$head" == *"$2"* ]]; }

out="$(stream | "$script" 2>/dev/null)"
check "test output is passed through" has "$out" "fast says hi"
check "plain text is passed through" has "$out" "build failed: plain text"
check "the report goes to stderr" lacks "$out" "slow-tests:"

err="$(stream | "$script" 2>&1 >/dev/null)"
check "a test over the threshold is listed" has "$err" "TestMedium"
check "the slowest test is listed" has "$err" "TestSlowest"
check "a fast test is not listed" lacks "$err" "TestFast"
check "a whole package is not listed" lacks "$err" "30.0s"
check "the slowest comes first" before "$err" "TestSlowest" "TestMedium"

err="$(stream | SLOW_TEST_SECONDS=5 "$script" 2>&1 >/dev/null)"
check "SLOW_TEST_SECONDS moves the threshold (TestSlowest stays)" has "$err" "TestSlowest"
check "SLOW_TEST_SECONDS moves the threshold (TestMedium goes)" lacks "$err" "TestMedium"

err="$(printf '%s\n' '{"Action":"pass","Package":"p","Test":"TestA","Elapsed":0.1}' | "$script" 2>&1 >/dev/null)"
check "nothing is reported when every test is fast" test -z "$err"

if ((fails > 0)); then exit 1; fi
