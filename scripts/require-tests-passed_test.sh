#!/usr/bin/env bash
# Tests for scripts/require-tests-passed.sh with canned `go test -v` output.
# Run: bash scripts/require-tests-passed_test.sh
set -euo pipefail

script="$(cd "$(dirname "$0")" && pwd)/require-tests-passed.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fails=0

# check <name> <command...>: ok when the command succeeds.
check() {
  local name="$1"
  shift
  if "$@"; then echo "ok: $name"; else echo "FAIL: $name"; fails=$((fails + 1)); fi
}
has() { [[ "$1" == *"$2"* ]]; }
lacks() { [[ "$1" != *"$2"* ]]; }
# run <file> <names...>: the exit status and stderr of the script, as "<status>|<stderr>".
run() {
  local err status=0
  err="$("$script" "$@" 2>&1 >/dev/null)" || status=$?
  printf '%s|%s' "$status" "$err"
}

cat >"$tmp/out.txt" <<'TXT'
=== RUN   TestReal
--- PASS: TestReal (1.20s)
=== RUN   TestSkipped
    onnx_test.go:36: MAM_FACESIM_MODEL_DIR is not set
--- SKIP: TestSkipped (0.00s)
=== RUN   TestBroken
--- FAIL: TestBroken (0.10s)
=== RUN   TestParent
=== RUN   TestParent/sub
    --- PASS: TestParent/sub (0.00s)
--- SKIP: TestParent (0.00s)
=== RUN   TestRealLonger
--- PASS: TestRealLonger (0.00s)
TXT
sed 's/$/\r/' "$tmp/out.txt" >"$tmp/crlf.txt"

r="$(run "$tmp/out.txt" TestReal TestRealLonger)"
check "tests that passed are accepted" has "$r" "0|"
r="$("$script" "$tmp/out.txt" TestReal 2>&1)"
check "success is said on stdout" has "$r" "all 1 required tests PASSED"

r="$(run "$tmp/out.txt" TestSkipped)"
check "a skipped test fails" has "$r" "1|"
check "a skipped test is named as skipped" has "$r" "TestSkipped did not pass: SKIPPED"
r="$(run "$tmp/out.txt" TestBroken)"
check "a failed test is named as failed" has "$r" "TestBroken did not pass: FAILED"
r="$(run "$tmp/out.txt" TestNowhere)"
check "a test missing from the output fails" has "$r" "TestNowhere did not pass: missing from the output"
r="$(run "$tmp/out.txt" TestReal TestSkipped TestNowhere)"
check "every test that did not pass is listed" has "$r" "2 of 3 required tests did not PASS"
check "a test that passed is not listed" lacks "$r" "TestReal did not pass"
r="$(run "$tmp/out.txt" Test)"
check "a name is no prefix match (Test is not TestReal)" has "$r" "Test did not pass: missing from the output"
r="$(run "$tmp/out.txt" TestParent)"
check "a passed subtest does not stand for its skipped parent" has "$r" "TestParent did not pass: SKIPPED"
r="$(run "$tmp/out.txt" TestParent/sub)"
check "a subtest is not counted" has "$r" "TestParent/sub did not pass: missing from the output"
r="$(run "$tmp/crlf.txt" TestReal TestRealLonger)"
check "CRLF line ends are accepted" has "$r" "0|"
r="$(run "$tmp/crlf.txt" TestSkipped)"
check "CRLF does not hide a skip" has "$r" "TestSkipped did not pass: SKIPPED"

status=0
"$script" - TestReal <"$tmp/out.txt" >/dev/null 2>&1 || status=$?
check "stdin is read with -" test "$status" -eq 0
printf '' >"$tmp/empty.txt"
r="$(run "$tmp/empty.txt" TestReal)"
check "an empty output fails (the tests did not run)" has "$r" "1|"
r="$(run "$tmp/nope.txt" TestReal)"
check "a missing file is a usage error" has "$r" "2|"
r="$(run "$tmp/out.txt")"
check "no test name is a usage error" has "$r" "2|"

if ((fails > 0)); then exit 1; fi
