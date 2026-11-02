#!/usr/bin/env bash
# Fails unless every named test PASSED in the output of `go test -v` (a file or stdin).
#
#   require-tests-passed.sh <go-test-v-output|-> <TestName>...
#
# `go test` exits 0 when the tests the CI job exists for were all skipped (SKIP is no failure): the `windows` job of
# .github/workflows/ci.yml runs the facesim tests that need the real DLL and models, and they skip when an environment
# variable did not get through. A named test that is skipped, failed or missing from the output is reported and fails here.
# Only top-level tests count (`--- PASS: TestName (0.00s)` at the start of a line); a subtest does not stand for its parent.
# Run the test of this script: bash scripts/require-tests-passed_test.sh
set -euo pipefail

usage() { echo "usage: require-tests-passed.sh <go-test-v-output|-> <TestName>..." >&2; exit 2; }
[[ $# -ge 2 ]] || usage
input="$1"
shift
[[ "$input" == "-" || -f "$input" ]] || { echo "require-tests-passed.sh: $input is not a file" >&2; exit 2; }

# \r is stripped: the output of a Windows runner may end its lines with CRLF.
results="$(tr -d '\r' <"${input/#-//dev/stdin}" | grep -E '^--- (PASS|FAIL|SKIP): ' || true)"

fails=0
for name in "$@"; do
  if grep -qE "^--- PASS: ${name} \(" <<<"$results"; then
    continue
  fi
  status="missing from the output"
  grep -qE "^--- FAIL: ${name} \(" <<<"$results" && status="FAILED"
  grep -qE "^--- SKIP: ${name} \(" <<<"$results" && status="SKIPPED"
  echo "require-tests-passed: ${name} did not pass: ${status}" >&2
  fails=$((fails + 1))
done
if ((fails > 0)); then
  echo "require-tests-passed: ${fails} of $# required tests did not PASS" >&2
  exit 1
fi
echo "require-tests-passed: all $# required tests PASSED"
