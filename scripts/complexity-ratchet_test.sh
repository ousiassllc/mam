#!/usr/bin/env bash
# Tests for the gocyclo / gocognit allowance list in .golangci.yml (complexity ratchet, #534 #537 #540).
# Each case writes one Go file into a throw-away module and lints it with the repo config:
# an allowed function at its recorded value passes; a bigger value, a same-named function in another
# file and a brand-new function over the limit all fail.
# Run: bash scripts/complexity-ratchet_test.sh   (needs golangci-lint, see `make tools`)
set -euo pipefail

config="$(cd "$(dirname "$0")/.." && pwd)/.golangci.yml"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
cd "$work"
printf 'module example.com/ratchet\n\ngo 1.22\n' >go.mod

fails=0
# gen <receiver or ""> <name> <n>: kind=cyc -> cyclomatic n+1 and cognitive n (n sequential ifs);
# kind=cog -> cognitive n (n >= 10) with a low cyclomatic complexity (4 nested loops + ifs inside them).
kind=cyc
gen() {
  local recv="$1" name="$2" n="$3" i k m
  [[ -n "$recv" ]] && recv="(r *$recv) "
  echo "func ${recv}${name}(x int) int {"
  if [[ "$kind" == cyc ]]; then
    for ((i = 0; i < n; i++)); do echo "	if x > $i {
		x++
	}"; done
  else
    k=$(((n - 10) / 5)) m=$(((n - 10) % 5))
    echo "	for x < 0 {
	for x < 1 {
	for x < 2 {
	for x < 3 {"
    for ((i = 0; i < k; i++)); do echo "	if x > $i {
		x++
	}"; done
    echo "	}
	}
	}
	}"
    for ((i = 0; i < m; i++)); do echo "	if x > $i {
		x++
	}"; done
  fi
  echo "	return x"
  echo "}"
}
# expect <name> <pass|fail> <file> <package> <receiver> <func name> <n>
expect() {
  local name="$1" want="$2" file="$3" pkg="$4" recv="$5" fn="$6" n="$7" rc=0 out
  rm -rf backend
  mkdir -p "$(dirname "$file")"
  {
    echo "package $pkg"
    [[ -n "$recv" ]] && echo "type $recv struct{}"
    gen "$recv" "$fn" "$n"
  } >"$file"
  out="$(golangci-lint run --config "$config" --enable-only gocyclo,gocognit --allow-parallel-runners ./... 2>&1)" || rc=$?
  if [[ ( "$want" == pass && "$rc" != 0 ) || ( "$want" == fail && ( "$rc" != 1 || "$out" != *"complexity"* ) ) ]]; then
    echo "FAIL: $name (want $want, exit $rc)"
    echo "$out"
    fails=$((fails + 1))
  else
    echo "ok: $name"
  fi
}

clean=backend/internal/agent/commandinterpreter/interpreter.go
describe=backend/internal/agent/modeladapter/prompt.go
views=backend/internal/service/chat_read.go
execute=backend/internal/service/director_execute.go
apply=backend/internal/repository/project.go

# Values below mirror the entries in .golangci.yml (n = complexity - 1 for gocyclo, = complexity for gocognit).
expect "clean at its recorded value passes" pass $clean commandinterpreter "" clean 26
expect "clean that got simpler (still over the limit) passes" pass $clean commandinterpreter "" clean 25
expect "clean that got more complex fails" fail $clean commandinterpreter "" clean 27
expect "describe at its recorded value passes" pass $describe modeladapter "" describe 30
expect "describe that got more complex fails" fail $describe modeladapter "" describe 31
expect "ChatService.views at its recorded value passes" pass $views service ChatService views 28
expect "ChatService.views that got more complex fails" fail $views service ChatService views 29
expect "DirectorService.execute at its recorded value passes" pass $execute service DirectorService execute 30
expect "DirectorService.execute that got more complex fails" fail $execute service DirectorService execute 31
kind=cog
expect "ProjectRepository.Apply at its recorded value passes" pass $apply repository ProjectRepository Apply 53
expect "ProjectRepository.Apply that got more complex fails" fail $apply repository ProjectRepository Apply 54

kind=cyc
expect "a same-named clean in another package fails" fail backend/internal/other/clean.go other "" clean 26
expect "a same-named describe in another package fails" fail backend/internal/other/describe.go other "" describe 25
expect "a new function over the gocyclo limit in an allowed file fails" fail $clean commandinterpreter "" other 25
kind=cog
expect "a new function over the gocognit limit fails" fail backend/internal/other/x.go other "" fn 51
expect "a function at the gocognit limit passes" pass backend/internal/other/x.go other "" fn 50
kind=cyc
expect "a function at the gocyclo limit passes" pass backend/internal/other/x.go other "" fn 24

if ((fails > 0)); then
  echo "$fails complexity ratchet test(s) failed"
  exit 1
fi
echo "complexity ratchet tests passed"
