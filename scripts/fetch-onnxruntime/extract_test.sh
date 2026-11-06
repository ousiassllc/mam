#!/usr/bin/env bash
# Tests for the success path of scripts/fetch-onnxruntime.sh and for the ways it opens the zip, without the network (#1073). A curl on
# PATH serves a made-up zip that matches the pins of a copy of the script (in a scratch repository, with the pins of the made-up files), so
# the files land in the directory whichever way the zip was opened. Each way of opening it (unzip, bsdtar, a tar that is bsdtar, 7z, the
# tar.exe of Windows) is tried with only that program on PATH, in the order the script prefers them. The refusals of a download that is
# not the pinned one are in scripts/fetch-onnxruntime_test.sh; the real download is checked by ci.yml's windows job.
# Run: bash scripts/fetch-onnxruntime/extract_test.sh
set -euo pipefail

repo="$(cd "$(dirname "$0")/../.." && pwd)"
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

# --- the success path and the ways of opening the zip -------------------------------------------------------------------------------
ortdir="onnxruntime-win-x64-1.23.2"
members=("$ortdir/lib/onnxruntime.dll" "$ortdir/lib/onnxruntime_providers_shared.dll" "$ortdir/LICENSE" "$ortdir/ThirdPartyNotices.txt")

# make_tree <dir> <dll content>: the files of the zip (the license texts are the committed ones, with CRLF as the release has them).
make_tree() {
  mkdir -p "$1/$ortdir/lib"
  printf '%s' "$2" >"$1/$ortdir/lib/onnxruntime.dll"
  printf 'providers' >"$1/$ortdir/lib/onnxruntime_providers_shared.dll"
  local f
  for f in LICENSE ThirdPartyNotices.txt; do
    sed 's/$/\r/' "$repo/build/windows/onnxruntime/$f" >"$1/$ortdir/$f"
  done
}

make_zip() { # make_zip <tree dir> <zip file>
  if command -v zip >/dev/null 2>&1; then
    (cd "$1" && zip -q -r "$2" "$ortdir")
  elif command -v python3 >/dev/null 2>&1; then
    (cd "$1" && python3 -c 'import sys, zipfile, pathlib
with zipfile.ZipFile(sys.argv[1], "w") as z:
    for p in sorted(pathlib.Path(sys.argv[2]).rglob("*")):
        if p.is_file():
            z.write(p)' "$2" "$ortdir")
  else
    echo "zip or python3 is required to make the test zip" >&2
    return 1
  fi
}

sha() { sha256sum "$1" | cut -d' ' -f1; }

# scratch_repo <case dir> <zip>: a repository that has a copy of the script whose pins are the ones of the made-up files, so that the
# zip is the pinned one; the pins of the DLLs are the ones of the good tree (so a zip with another DLL is refused).
scratch_repo() {
  local dir="$1" zip="$2"
  mkdir -p "$dir/repo/scripts" "$dir/repo/build/windows"
  cp -R "$repo/build/windows/onnxruntime" "$dir/repo/build/windows/"
  sed -e "s/^zip_sha256=.*/zip_sha256=\"$(sha "$zip")\"/" \
    -e "s/^dll_sha256=.*/dll_sha256=\"$(sha "$work/good/$ortdir/lib/onnxruntime.dll")\"/" \
    -e "s/^providers_sha256=.*/providers_sha256=\"$(sha "$work/good/$ortdir/lib/onnxruntime_providers_shared.dll")\"/" \
    "$script" >"$dir/repo/scripts/fetch-onnxruntime.sh"
}

make_tree "$work/good" "the good dll"
make_zip "$work/good" "$work/good.zip"
make_tree "$work/otherdll" "another dll"
make_zip "$work/otherdll" "$work/otherdll.zip"
make_tree "$work/otherlicense" "the good dll"
echo "an edited line" >>"$work/otherlicense/$ortdir/LICENSE"
make_zip "$work/otherlicense" "$work/otherlicense.zip"

# A curl that serves $FAKE_ZIP (and says so in $FAKE_LOG), and a program that opens the zip by copying the made-up files ($FAKE_TREE) to
# the directory it is given (-d, -C or -o<dir>), saying how it was called. A tar that is not bsdtar (GNU tar) cannot open a zip.
cat >"$work/curl-serving-zip" <<'STUB'
#!/usr/bin/env bash
echo "curl" >>"$FAKE_LOG"
out=""
while [[ $# -gt 0 ]]; do
  if [[ "$1" == "-o" ]]; then out="$2"; shift; fi
  shift
done
cp "$FAKE_ZIP" "$out"
STUB
cat >"$work/opener" <<'STUB'
#!/usr/bin/env bash
me="${0##*/}"
if [[ "$me" == tar && "${1:-}" == "--version" ]]; then echo "${FAKE_TAR_VERSION:-tar (GNU tar) 1.35}"; exit 0; fi
echo "$me $*" >>"$FAKE_LOG"
[[ "$me" == tar && "${FAKE_TAR_VERSION:-}" != bsdtar* ]] && exit 2
dest=""
args=("$@")
for ((i = 0; i < ${#args[@]}; i++)); do
  case "${args[i]}" in
    -d | -C) dest="${args[i + 1]}" ;;
    -o*) dest="${args[i]#-o}" ;;
  esac
done
cp -R "$FAKE_TREE/." "$dest/"
STUB
# They run with a PATH that has no env: the shebang names this bash.
sed -i.bak "1s|.*|#!$BASH|" "$work/curl-serving-zip" "$work/opener"
chmod +x "$work/curl-serving-zip" "$work/opener"

# fetch_with <case> <zip> <tree> <programs...>: runs the scratch script into $work/<case>/dest with a PATH that has only the tools the
# script needs, curl that serves the zip, and the listed programs (unzip, bsdtar, tar, 7z, or tar.exe under SYSTEMROOT/System32). Output
# is in $work/<case>/out, the calls in $work/<case>/log, the exit status is the function's.
fetch_with() {
  local c="$work/$1" zip="$2" tree="$3" p t
  shift 3
  rm -rf "$c" && mkdir -p "$c/bin" "$c/sys/System32"
  for t in dirname cut mktemp rm tr cmp mkdir cp grep sha256sum; do ln -s "$(command -v "$t")" "$c/bin/$t"; done
  ln -s "$work/curl-serving-zip" "$c/bin/curl"
  for p in "$@"; do
    if [[ "$p" == tar.exe ]]; then ln -s "$work/opener" "$c/sys/System32/tar.exe"; else ln -s "$work/opener" "$c/bin/$p"; fi
  done
  scratch_repo "$c" "$zip"
  : >"$c/log"
  FAKE_LOG="$c/log" FAKE_ZIP="$zip" FAKE_TREE="$tree/$ortdir/.." SYSTEMROOT="$c/sys" PATH="$c/bin" \
    "$BASH" "$c/repo/scripts/fetch-onnxruntime.sh" "$c/dest" >"$c/out" 2>&1
}

# used <case> <program>: the first call of the log is the program's (and curl came before it).
used() { [[ "$(grep -v '^curl$' "$work/$1/log" | cut -d' ' -f1 | sed -n 1p 2>/dev/null)" == "$2" ]]; }
installed() { # installed <case>: the two DLLs are the made-up ones, and nothing else is left in the directory
  local d="$work/$1/dest"
  cmp -s "$d/onnxruntime.dll" "$work/good/$ortdir/lib/onnxruntime.dll" && cmp -s "$d/onnxruntime_providers_shared.dll" "$work/good/$ortdir/lib/onnxruntime_providers_shared.dll" \
    && [[ "$(find "$d" -type f | wc -l)" -eq 2 ]] && grep -q "ONNX Runtime 1.23.2 is in $d" "$work/$1/out"
}
opened_with_the_members() { # opened_with_the_members <case>: the call named the four members of the zip
  local m
  for m in "${members[@]}"; do grep -qF "$m" "$work/$1/log" || return 1; done
}

way_is_used() { # way_is_used <case> <program> <programs on PATH...>
  local c="$1" prog="$2"
  shift 2
  fetch_with "$c" "$work/good.zip" "$work/good" "$@" && installed "$c" && used "$c" "$prog" && opened_with_the_members "$c"
}
unzip_is_preferred() { way_is_used unzip-first unzip unzip bsdtar tar 7z tar.exe; }
bsdtar_when_no_unzip() { FAKE_TAR_VERSION="bsdtar 3.7.2" way_is_used bsdtar-next bsdtar bsdtar tar 7z tar.exe; }
tar_that_is_bsdtar() { FAKE_TAR_VERSION="bsdtar 3.7.2" way_is_used tar-next tar tar 7z tar.exe; }
gnu_tar_is_skipped_for_7z() { FAKE_TAR_VERSION="tar (GNU tar) 1.35" way_is_used gnutar-skip 7z tar 7z tar.exe && ! grep -q '^tar ' "$work/gnutar-skip/log"; }
seven_zip_gets_a_native_directory() { way_is_used sevenzip 7z 7z && grep -qE '^7z x -y -bso0 -bsp0 -o.*/x ' "$work/sevenzip/log"; }
windows_tar_exe_is_the_last_resort() { way_is_used windows-tar tar.exe tar.exe; }
no_way_to_open_the_zip() {
  ! fetch_with no-opener "$work/good.zip" "$work/good" tar && [[ ! -e "$work/no-opener/dest" ]] \
    && grep -q "unzip, bsdtar (tar.exe) or 7z is required" "$work/no-opener/out"
}
unexpected_dll_is_refused() {
  ! fetch_with other-dll "$work/otherdll.zip" "$work/otherdll" unzip && grep -q "unexpected onnxruntime.dll" "$work/other-dll/out" && [[ ! -e "$work/other-dll/dest" ]]
}
changed_license_is_refused() {
  ! fetch_with other-license "$work/otherlicense.zip" "$work/otherlicense" unzip && grep -q "LICENSE of $ortdir.zip differs" "$work/other-license/out" && [[ ! -e "$work/other-license/dest" ]]
}
present_files_are_not_fetched_again() {
  fetch_with again "$work/good.zip" "$work/good" unzip || return 1
  local before
  before="$(wc -l <"$work/again/log")"
  FAKE_LOG="$work/again/log" FAKE_ZIP="$work/good.zip" FAKE_TREE="$work/good" SYSTEMROOT="$work/again/sys" PATH="$work/again/bin" \
    "$BASH" "$work/again/repo/scripts/fetch-onnxruntime.sh" "$work/again/dest" >"$work/again/out2" 2>&1 \
    && [[ "$(wc -l <"$work/again/log")" -eq "$before" ]] && grep -q "^ok  .*(ONNX Runtime 1.23.2)" "$work/again/out2"
}
real_unzip_opens_the_zip() { # the real program, when there is one: the arguments the script gives it are right
  command -v unzip >/dev/null 2>&1 || { echo "skip: no unzip on this machine (the stubbed one above has the arguments)"; return 0; }
  local c="$work/real-unzip"
  rm -rf "$c" && mkdir -p "$c/bin"
  local t
  for t in dirname cut mktemp rm tr cmp mkdir cp grep sha256sum unzip; do ln -s "$(command -v "$t")" "$c/bin/$t"; done
  ln -s "$work/curl-serving-zip" "$c/bin/curl"
  scratch_repo "$c" "$work/good.zip"
  FAKE_LOG="$c/log" FAKE_ZIP="$work/good.zip" PATH="$c/bin" "$BASH" "$c/repo/scripts/fetch-onnxruntime.sh" "$c/dest" >"$c/out" 2>&1 && installed real-unzip
}

check "the pinned zip is opened with unzip first, and the DLLs land in the directory" pass unzip_is_preferred
check "without unzip, bsdtar opens it" pass bsdtar_when_no_unzip
check "without unzip and bsdtar, a tar that is bsdtar opens it" pass tar_that_is_bsdtar
check "a tar that is GNU tar is skipped for 7z" pass gnu_tar_is_skipped_for_7z
check "7z is given a directory in its own -o form and the members" pass seven_zip_gets_a_native_directory
check "the tar.exe of Windows opens it when nothing else can" pass windows_tar_exe_is_the_last_resort
check "without any program that opens a zip it is an error and leaves nothing" pass no_way_to_open_the_zip
check "a zip with another DLL than the pinned one is refused and leaves nothing" pass unexpected_dll_is_refused
check "a zip with license texts that are not the committed ones is refused and leaves nothing" pass changed_license_is_refused
check "DLLs that are already there are not fetched again" pass present_files_are_not_fetched_again
check "the real unzip opens the zip with the arguments of the script" pass real_unzip_opens_the_zip

if [[ "$fails" -gt 0 ]]; then
  echo "$fails failed"
  exit 1
fi
