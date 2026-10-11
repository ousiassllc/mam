#!/usr/bin/env bash
# Reads `go test -json` on stdin: prints the tests' own output as `go test` would, then lists the tests
# that took longer than SLOW_TEST_SECONDS (default 3) so that a slow test shows up in the CI log
# before it makes the package the slowest one. A warning only: the exit status is `go test`'s
# (run it as `bash -o pipefail -c 'go test -json ./... | scripts/slow-tests.sh'`).
# A test that calls t.Parallel also counts the time it waited for its turn, so on a busy machine
# its time is an upper bound.
# Run the test of this script: bash scripts/slow-tests_test.sh
set -euo pipefail

command -v jq >/dev/null || { echo "slow-tests.sh: jq is required (https://jqlang.github.io/jq/)" >&2; exit 2; }

threshold="${SLOW_TEST_SECONDS:-3}"
max_listed=20
raw="$(mktemp)"
trap 'rm -f "$raw"' EXIT

# Lines that are not JSON (a build failure is printed as plain text) are passed through unchanged.
tee "$raw" | jq -R -r 'try (fromjson | select(.Action == "output") | .Output | rtrimstr("\n")) catch .'

slow="$(jq -R -r --argjson t "$threshold" '
  try (fromjson | select(.Action == "pass" and .Test != null and .Elapsed > $t)
      | "\(.Elapsed)\t\(.Package | sub("^.*/internal/"; ""))\t\(.Test)") catch empty
' "$raw" | sort -rn)"
if [[ -n "$slow" ]]; then
  {
    echo
    echo "slow-tests: tests over ${threshold}s (slowest ${max_listed}; a slow test costs every run, so speed it up or share its fixtures: testenv.FFmpegFile, testenv.Parallel)"
    head -n "$max_listed" <<<"$slow" | awk -F'\t' '{printf "  %8.1fs  %s  %s\n", $1, $2, $3}'
  } >&2
fi
