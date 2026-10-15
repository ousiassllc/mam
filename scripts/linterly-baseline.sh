#!/usr/bin/env bash
# Line-limit ratchet: the files / directories already over the linterly limit (.linterly.yml) are
# recorded in .linterly-baseline, and CI fails only when the situation gets worse than that record.
#
#   linterly-baseline.sh [check]   fail when a file / directory is over the limit and is not in the
#                                  baseline, or is longer than its baseline line count. Entries that
#                                  shrank or got under the limit are reported (stale baseline) but do
#                                  not fail: lower the baseline with `update`.
#   linterly-baseline.sh update    rewrite the baseline from the current `linterly check` result.
#                                  Review the diff: it must only remove entries or lower numbers.
#
# "Over the limit" is linterly's own `error` severity (above the limit plus warning_threshold), the
# same verdict that makes `linterly check` exit 1 and that scripts/linterly-staged.sh blocks on.
# Baseline format: `<lines><TAB><path>` per line, sorted by path; directories end with `/`;
# lines starting with `#` are comments.
#
# Environment (for tests): LINTERLY_BASELINE overrides the baseline path.
set -euo pipefail

cd "$(dirname "$0")/.."
mode="${1:-check}"
baseline="${LINTERLY_BASELINE:-.linterly-baseline}"

command -v linterly >/dev/null || { echo "linterly not found: make tools" >&2; exit 1; }
command -v jq >/dev/null || { echo "jq not found" >&2; exit 1; }

report="$(mktemp)"
trap 'rm -f "$report"' EXIT
linterly check --no-update-check --format json >"$report" || true # exits 1 while anything is over the limit; a broken run shows up as invalid JSON below
jq -e '.results' "$report" >/dev/null || { echo "linterly: no usable report" >&2; exit 1; }

# `<lines><TAB><path>` of everything over the limit, sorted bytewise so the file diffs cleanly.
current() {
  jq -r '.results[] | select(.severity == "error") | "\(.lines)\t\(.path)"' "$report" | LC_ALL=C sort -t$'\t' -k2
}

case "$mode" in
  update)
    {
      echo "# linterly の行数上限（.linterly.yml）を超えている既存のファイル・ディレクトリ。scripts/linterly-baseline.sh が CI で悪化を検出する基準値。"
      echo "# 分割して減らしたら \`make linterly-baseline\` で書き換える（差分は削除か数値の減少だけであること）。形式: <行数><TAB><パス>"
      current
    } >"$baseline"
    echo "linterly: wrote $(current | wc -l | tr -d ' ') entries to $baseline"
    ;;
  check)
    [[ -f "$baseline" ]] || { echo "linterly: $baseline not found: run scripts/linterly-baseline.sh update" >&2; exit 1; }
    verdict="$(jq -rn --rawfile base "$baseline" --slurpfile report "$report" '
      ($base | split("\n") | map(select(length > 0 and (startswith("#") | not)) | split("\t") | {key: .[1], value: (.[0] | tonumber)}) | from_entries) as $b
      | [$report[0].results[] | select(.severity == "error")] as $over
      | ($over | map({key: .path, value: .lines}) | from_entries) as $now
      | ($over[] | select($b[.path] == null) | "new\t\(.path)\t\(.lines) lines (limit \(.limit)), not in the baseline"),
        ($over[] | select($b[.path] != null and .lines > $b[.path]) | "new\t\(.path)\t\($b[.path]) -> \(.lines) lines, longer than the baseline"),
        ($b | to_entries[] | select($now[.key] == null) | "stale\t\(.key)\tunder the limit now (baseline \(.value) lines)"),
        ($over[] | select($b[.path] != null and .lines < $b[.path]) | "stale\t\(.path)\t\($b[.path]) -> \(.lines) lines")')"
    worse="$(printf '%s\n' "$verdict" | grep -P '^new\t' | cut -f2- | tr '\t' ' ' || true)"
    stale="$(printf '%s\n' "$verdict" | grep -P '^stale\t' | cut -f2- | tr '\t' ' ' || true)"
    if [[ -n "$stale" ]]; then
      echo "linterly: baseline can be lowered (run 'make linterly-baseline' and commit):" >&2
      printf '%s\n' "$stale" | sed 's/^/  /' >&2
    fi
    if [[ -n "$worse" ]]; then
      echo "linterly: over the line limit and worse than the baseline (split the file / package, or see spec/environment/setup.md):" >&2
      printf '%s\n' "$worse" | sed 's/^/  /' >&2
      exit 1
    fi
    echo "linterly: no regression against $baseline"
    ;;
  *)
    echo "usage: $0 [check|update]" >&2
    exit 2
    ;;
esac
