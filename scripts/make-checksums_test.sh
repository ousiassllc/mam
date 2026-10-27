#!/usr/bin/env bash
# Tests for scripts/make-checksums.sh with dummy files.
# Run: bash scripts/make-checksums_test.sh
set -euo pipefail

script="$(cd "$(dirname "$0")" && pwd)/make-checksums.sh"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
fails=0

check() { # check <name> <expected: pass|fail> <command...>
  local name="$1" want="$2"
  shift 2
  local got=fail
  if "$@"; then got=pass; fi
  if [[ "$got" == "$want" ]]; then echo "ok: $name"; else echo "FAIL: $name (wanted $want)"; fails=$((fails + 1)); fi
}

fresh_dir() {
  rm -rf "$work/dist" && mkdir "$work/dist"
  printf 'installer' >"$work/dist/mam-1.2.3-windows-amd64-setup.exe"
  printf 'portable' >"$work/dist/mam-1.2.3-windows-amd64.exe"
  printf 'dmg' >"$work/dist/mam-1.2.3-darwin-arm64.dmg"
}

both_installers_are_listed_and_verify() {
  fresh_dir
  bash "$script" "$work/dist" 1.2.3 >/dev/null 2>&1 || return 1
  [[ "$(wc -l <"$work/dist/checksums.txt")" -eq 2 ]] || return 1
  grep -q -- '  mam-1.2.3-windows-amd64-setup.exe$' "$work/dist/checksums.txt" || return 1
  grep -q -- '  mam-1.2.3-darwin-arm64.dmg$' "$work/dist/checksums.txt" || return 1
  (cd "$work/dist" && sha256sum -c checksums.txt >/dev/null 2>&1)
}
portable_exe_is_not_listed() { ! grep -q 'windows-amd64.exe' "$work/dist/checksums.txt"; }
missing_dmg() { fresh_dir && rm "$work/dist/mam-1.2.3-darwin-arm64.dmg" && bash "$script" "$work/dist" 1.2.3 >/dev/null 2>&1; }
missing_installer() { fresh_dir && rm "$work/dist/mam-1.2.3-windows-amd64-setup.exe" && bash "$script" "$work/dist" 1.2.3 >/dev/null 2>&1; }
other_version() { fresh_dir && bash "$script" "$work/dist" 1.2.4 >/dev/null 2>&1; }
bad_version() { fresh_dir && bash "$script" "$work/dist" v1.2.3 >/dev/null 2>&1; }
no_dir() { bash "$script" "$work/nodir" 1.2.3 >/dev/null 2>&1; }

check "both installers are listed and verify with sha256sum -c" pass both_installers_are_listed_and_verify
check "the portable exe is not listed" pass portable_exe_is_not_listed
check "a missing dmg fails" fail missing_dmg
check "a missing Windows installer fails" fail missing_installer
check "files of another version fail" fail other_version
check "a version with a v prefix fails" fail bad_version
check "a missing directory fails" fail no_dir

if [[ "$fails" -gt 0 ]]; then
  echo "$fails failed"
  exit 1
fi
