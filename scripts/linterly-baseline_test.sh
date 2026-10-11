#!/usr/bin/env bash
# Tests for scripts/linterly-baseline.sh with a stubbed `linterly` (the report is a JSON file).
# Run: bash scripts/linterly-baseline_test.sh
set -euo pipefail

script="$(cd "$(dirname "$0")" && pwd)/linterly-baseline.sh"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
mkdir "$work/bin"
cat >"$work/bin/linterly" <<'STUB'
#!/usr/bin/env bash
cat "$LINTERLY_STUB_REPORT"
exit 1
STUB
chmod +x "$work/bin/linterly"
export PATH="$work/bin:$PATH"
export LINTERLY_STUB_REPORT="$work/report.json"
export LINTERLY_BASELINE="$work/baseline"

fails=0
# report <path>:<type>:<lines>:<severity> ...
report() {
  local entries=() e
  for e in "$@"; do
    IFS=: read -r p t l s <<<"$e"
    entries+=("{\"path\":\"$p\",\"type\":\"$t\",\"lines\":$l,\"limit\":300,\"threshold\":330,\"severity\":\"$s\"}")
  done
  printf '{"results":[%s],"summary":{},"warnings":[]}' "$(IFS=,; echo "${entries[*]}")" >"$LINTERLY_STUB_REPORT"
}
# expect <name> <want exit code> <want stderr substring or ""> [mode]
expect() {
  local name="$1" want="$2" sub="$3" mode="${4:-check}" rc=0 err
  err="$("$script" "$mode" 2>&1 >/dev/null)" || rc=$?
  if [[ "$rc" != "$want" || "$err" != *"$sub"* ]]; then
    echo "FAIL: $name (exit $rc, want $want; want stderr to contain '$sub'; got: $err)"
    fails=$((fails + 1))
  else
    echo "ok: $name"
  fi
}

report a.go:file:400:error b.go:file:250:pass d/:directory:2500:error c.go:file:310:warn
"$script" update >/dev/null
if [[ "$(grep -v '^#' "$LINTERLY_BASELINE")" != $'2500\td/\n400\ta.go' && "$(grep -v '^#' "$LINTERLY_BASELINE")" != $'400\ta.go\n2500\td/' ]]; then
  echo "FAIL: update records exactly the error entries (got: $(grep -v '^#' "$LINTERLY_BASELINE"))"
  fails=$((fails + 1))
else
  echo "ok: update records exactly the error entries"
fi

expect "unchanged state passes" 0 ""

report a.go:file:401:error d/:directory:2500:error
expect "a baseline file that grew fails" 1 "a.go 400 -> 401 lines"

report a.go:file:400:error d/:directory:2501:error
expect "a baseline directory that grew fails" 1 "d/ 2500 -> 2501 lines"

report a.go:file:400:error d/:directory:2500:error new.go:file:500:error
expect "a new file over the limit fails" 1 "new.go 500 lines (limit 300), not in the baseline"

report a.go:file:400:error d/:directory:2500:error new/:directory:2100:error
expect "a new directory over the limit fails" 1 "new/ 2100 lines (limit 300), not in the baseline"

report a.go:file:350:error d/:directory:2500:error new.go:file:320:warn
expect "a shrunk file and a new warn-only file pass, the stale entry is reported" 0 "a.go 400 -> 350 lines"

report a.go:file:250:pass d/:directory:2500:error
expect "a file that got under the limit passes and is reported stale" 0 "a.go under the limit now"

report a.go:file:450:error d/:directory:1500:pass new.go:file:600:error
expect "growth fails even when something else shrank" 1 "new.go 600 lines"

rm "$LINTERLY_BASELINE"
expect "a missing baseline fails" 1 "not found"

printf 'not json' >"$LINTERLY_STUB_REPORT"
expect "an unusable linterly report fails" 1 "no usable report"

report a.go:file:400:error
expect "an unknown mode is a usage error" 2 "usage" bogus

report a.go:file:350:error
"$script" update >/dev/null
if [[ "$(grep -v '^#' "$LINTERLY_BASELINE")" != $'350\ta.go' ]]; then
  echo "FAIL: update lowers the baseline (got: $(grep -v '^#' "$LINTERLY_BASELINE"))"
  fails=$((fails + 1))
else
  echo "ok: update lowers the baseline"
fi

[[ "$fails" -eq 0 ]] || { echo "$fails test(s) failed" >&2; exit 1; }
