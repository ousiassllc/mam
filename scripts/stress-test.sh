#!/usr/bin/env bash
# Runs a Go test many times under -race to find a flake, without loading the machine more than a few
# processes: the shared host (and the CI runner) must stay usable. One test binary is built, then JOBS
# copies run side by side, each doing its share of COUNT runs.
#
#   scripts/stress-test.sh [-j JOBS] [-n COUNT] [-c CPUS] [-r REGEX] [-d] PACKAGE
#
#   -j JOBS   processes at once. Default 2, at most 4 (more is refused, not rounded down: a stress run that
#             silently uses fewer processes than asked would not be the run that was asked for).
#   -n COUNT  runs in total (default 20), split across the jobs.
#   -c CPUS   pin every job to these CPUs (taskset -c; e.g. 0 or 0,1). Squeezing the CPU instead of adding
#             processes is how a load flake is reproduced here: 2 jobs on 1 CPU starve each other the way
#             24 packages on a busy machine do.
#   -r REGEX  tests to run (go test -run). Default: all tests of the package.
#   -d        print the plan and stop (nothing is built or run).
#
# Exit status: 0 when every run passed, 1 when a run failed, 2 on a usage error.
set -euo pipefail

max_jobs=4
jobs=2
count=20
cpus=""
run=""
dry=0

usage() {
  sed -n '2,/^set -euo/p' "$0" | sed '$d' | sed 's/^# \{0,1\}//' >&2
  exit 2
}
die() {
  echo "stress-test: $*" >&2
  exit 2
}
positive() { [[ "$1" =~ ^[1-9][0-9]*$ ]]; }

while getopts "j:n:c:r:dh" opt; do
  case "$opt" in
    j) jobs="$OPTARG" ;;
    n) count="$OPTARG" ;;
    c) cpus="$OPTARG" ;;
    r) run="$OPTARG" ;;
    d) dry=1 ;;
    *) usage ;;
  esac
done
shift $((OPTIND - 1))
[[ $# -eq 1 ]] || usage
pkg="$1"

positive "$jobs" || die "-j must be a positive integer, got '$jobs'"
positive "$count" || die "-n must be a positive integer, got '$count'"
((jobs <= max_jobs)) || die "-j $jobs is more than the limit of $max_jobs processes (a stress run must not load a shared machine)"
[[ -z "$cpus" || "$cpus" =~ ^[0-9,-]+$ ]] || die "-c must be a CPU list such as 0 or 0,1, got '$cpus'"
((jobs <= count)) || jobs="$count"

# Runs per job: COUNT / JOBS, the first COUNT % JOBS jobs take one more.
per_job() { echo $((count / jobs + ($1 <= count % jobs ? 1 : 0))); }

echo "stress-test: package=$pkg jobs=$jobs count=$count run=${run:-<all>} cpus=${cpus:-<any>}" >&2
((dry == 0)) || exit 0
[[ -z "$cpus" ]] || command -v taskset >/dev/null || die "-c needs taskset (Linux)"

dir="$(go list -f '{{.Dir}}' "$pkg")"
work="$(mktemp -d)"
pids=()
cleanup() {
  local pid
  for pid in ${pids[@]+"${pids[@]}"}; do kill "$pid" 2>/dev/null || true; done # only the jobs this script started
  rm -rf "$work"
}
trap cleanup EXIT

bin="$work/test.bin"
go test -race -c -o "$bin" "$pkg"
cd "$dir" # tests read testdata relative to their package

for i in $(seq 1 "$jobs"); do
  args=("-test.count=$(per_job "$i")" -test.timeout=30m)
  [[ -z "$run" ]] || args+=("-test.run=$run")
  if [[ -n "$cpus" ]]; then
    taskset -c "$cpus" "$bin" "${args[@]}" >"$work/job$i.log" 2>&1 &
  else
    "$bin" "${args[@]}" >"$work/job$i.log" 2>&1 &
  fi
  pids+=("$!")
done

failed=0
for i in $(seq 1 "$jobs"); do
  if wait "${pids[$((i - 1))]}"; then
    echo "stress-test: job $i: ok ($(per_job "$i") runs)" >&2
  else
    echo "stress-test: job $i: FAILED" >&2
    cat "$work/job$i.log" >&2
    failed=1
  fi
done
((failed == 0)) || exit 1
echo "stress-test: $count runs, 0 failures" >&2
