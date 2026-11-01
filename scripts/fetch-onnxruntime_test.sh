#!/usr/bin/env bash
# Tests for scripts/fetch-onnxruntime.sh without the network: a curl on PATH serves a made-up zip, so the checksum check must refuse it.
# The real download is checked by ci.yml's windows job (it fetches the pinned zip and runs the facesim tests on the DLL).
# Run: bash scripts/fetch-onnxruntime_test.sh
set -euo pipefail

repo="$(cd "$(dirname "$0")/.." && pwd)"
script="$repo/scripts/fetch-onnxruntime.sh"
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

# A curl that writes the file of -o and does what $FAKE_CURL says: "garbage" serves bytes, "fail" exits 22 (HTTP error).
mkdir "$work/bin"
cat >"$work/bin/curl" <<'STUB'
#!/usr/bin/env bash
out=""
while [[ $# -gt 0 ]]; do
  if [[ "$1" == "-o" ]]; then out="$2"; shift; fi
  shift
done
[[ "$FAKE_CURL" == "fail" ]] && exit 22
printf 'not the pinned zip' >"$out"
STUB
chmod +x "$work/bin/curl"

version_is_printed() { [[ "$(bash "$script" --version)" =~ ^1\.23\.[0-9]+$ ]]; }
no_argument() { bash "$script" >/dev/null 2>&1; }
empty_argument() { bash "$script" "" >/dev/null 2>&1; }
wrong_download_is_refused() {
  local out
  out="$(FAKE_CURL=garbage PATH="$work/bin:$PATH" bash "$script" "$work/dest" 2>&1)" && return 1
  [[ "$out" == *"checksum mismatch"* ]] && [[ ! -e "$work/dest" ]]
}
failed_download_is_an_error() {
  ! FAKE_CURL=fail PATH="$work/bin:$PATH" bash "$script" "$work/dest2" >/dev/null 2>&1 && [[ ! -e "$work/dest2" ]]
}
licenses_are_committed_with_lf() {
  local f
  for f in LICENSE ThirdPartyNotices.txt; do
    [[ -s "$repo/build/windows/onnxruntime/$f" ]] || return 1
    ! grep -q $'\r' "$repo/build/windows/onnxruntime/$f" || return 1
  done
  head -n 1 "$repo/build/windows/onnxruntime/LICENSE" | grep -q '^MIT License$'
}

check "the pinned version is printed" pass version_is_printed
check "no argument is a usage error" fail no_argument
check "an empty directory name is a usage error" fail empty_argument
check "a download that is not the pinned zip is refused and leaves nothing" pass wrong_download_is_refused
check "a failed download is an error and leaves nothing" pass failed_download_is_an_error
check "the ONNX Runtime license texts are committed (MIT, LF)" pass licenses_are_committed_with_lf

if [[ "$fails" -gt 0 ]]; then
  echo "$fails failed"
  exit 1
fi
