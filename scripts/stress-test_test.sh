#!/usr/bin/env bash
# Tests for scripts/stress-test.sh with a stand-in `go` (no real build, no real test run).
# Run: bash scripts/stress-test_test.sh
set -euo pipefail

script="$(cd "$(dirname "$0")" && pwd)/stress-test.sh"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
fails=0

# The stand-in go: `go list -f {{.Dir}} PKG` answers a directory, `go test -race -c -o BIN PKG` writes a
# test binary that records its arguments in $STUB_LOG and exits with $STUB_EXIT.
mkdir "$work/bin" "$work/pkg"
cat >"$work/bin/go" <<'STUB'
#!/usr/bin/env bash
if [[ "$1" == list ]]; then
  echo "$STUB_PKG_DIR"
  exit 0
fi
out=""
while (($#)); do
  [[ "$1" == -o ]] && out="$2"
  shift
done
cat >"$out" <<'BIN'
#!/usr/bin/env bash
echo "$*" >>"$STUB_LOG"
exit "${STUB_EXIT:-0}"
BIN
chmod +x "$out"
STUB
chmod +x "$work/bin/go"

# run <args...>: stress-test.sh with the stand-in go; sets $out (stderr) and $rc.
run() {
  : >"$work/calls"
  rc=0
  out="$(PATH="$work/bin:$PATH" STUB_PKG_DIR="$work/pkg" STUB_LOG="$work/calls" "$script" "$@" 2>&1)" || rc=$?
}
check() {
  local name="$1"
  shift
  if "$@"; then echo "ok: $name"; else echo "FAIL: $name"; fails=$((fails + 1)); fi
}
has() { [[ "$1" == *"$2"* ]]; }
eq() { [[ "$1" == "$2" ]]; }

run -d ./x
check "the default is 2 jobs" has "$out" "jobs=2 count=20"
check "a dry run runs nothing" eq "$rc" 0

for jobs in 5 24 100; do
  run -j "$jobs" ./x
  check "-j $jobs is refused" eq "$rc" 2
  check "-j $jobs says why" has "$out" "limit of 4"
  check "-j $jobs starts nothing" eq "$(wc -l <"$work/calls")" 0
done
for bad in 0 -1 two 1.5 ""; do
  run -j "$bad" ./x
  check "-j '$bad' is refused" eq "$rc" 2
done
run -n 0 ./x
check "-n 0 is refused" eq "$rc" 2
run -c "0;rm" ./x
check "a CPU list with a shell metacharacter is refused" eq "$rc" 2
run
check "no package is a usage error" eq "$rc" 2

run -j 4 -d ./x
check "the limit itself is allowed" has "$out" "jobs=4"

run -j 3 -n 10 -r TestX ./x
check "the runs of the jobs add up to -n" eq "$(grep -o 'test.count=[0-9]*' "$work/calls" | cut -d= -f2 | awk '{ s += $1 } END { print s }')" 10
check "one process per job" eq "$(wc -l <"$work/calls")" 3
check "the test pattern reaches the binary" has "$(cat "$work/calls")" "-test.run=TestX"
check "all green is exit 0" eq "$rc" 0

run -j 4 -n 2 ./x
check "never more jobs than runs" eq "$(wc -l <"$work/calls")" 2

rc=0
out="$(PATH="$work/bin:$PATH" STUB_PKG_DIR="$work/pkg" STUB_LOG="$work/calls" STUB_EXIT=1 "$script" -n 2 ./x 2>&1)" || rc=$?
check "a failing run is exit 1" eq "$rc" 1
check "a failing run is reported" has "$out" "FAILED"

if command -v taskset >/dev/null; then
  run -c 0 -n 2 ./x
  check "a run pinned to a CPU still runs every job" eq "$(wc -l <"$work/calls")" 2
fi

if ((fails > 0)); then
  exit 1
fi
