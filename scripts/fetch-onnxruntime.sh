#!/usr/bin/env bash
# Downloads the Windows x64 build of ONNX Runtime (MIT, Microsoft) that the Windows installer bundles next to mam.exe for the
# AI Video Judge's Face Identity Similarity, verifies its SHA-256, and puts the DLLs into a directory. The source is Microsoft's
# official NuGet package Microsoft.ML.OnnxRuntime on nuget.org (a .nupkg is a zip; the DLLs are in runtimes/win-x64/native): the
# GitHub releases of microsoft/onnxruntime stopped answering (404 on 2026-11-19, Issue #1173), which broke the Windows CI job.
# Usage: scripts/fetch-onnxruntime.sh <DIR>     DIR gets onnxruntime.dll and onnxruntime_providers_shared.dll
#        scripts/fetch-onnxruntime.sh --version prints the pinned ONNX Runtime version (scripts/third-party-notices.sh uses it)
# Called by scripts/package-windows.sh (the installer) and by ci.yml's windows job (the tests with the real DLL).
# The version is pinned together with the SHA-256 of the .nupkg, the one of each DLL and the license texts committed in
# build/windows/onnxruntime/: to raise the version, change the four pins, replace those two files with the ones of the new package,
# and run scripts/third-party-notices.sh. The bindings (github.com/shota3506/onnxruntime-purego) speak ONNX Runtime C API
# version 23, which is 1.23.x (backend/internal/facesim/onnx.go ortAPIVersion).
# See spec/architecture/model-adapters.md §10.1 and spec/architecture/desktop.md §4.
# Needs: curl, sha256sum or shasum, and unzip, bsdtar (tar.exe of Windows and macOS is one), or 7z (to open the .nupkg).
set -euo pipefail

ort_version="1.23.2"
# https://www.nuget.org/packages/Microsoft.ML.OnnxRuntime/1.23.2 (nuget.org lists its SHA-512 as sVAnmG5x...Fgs2mA==, 123371574 bytes).
pkg_sha256="25fe172f7fcdf34f2b5b02c8b997f6b88abc282956849d4af4335af23d0c4e4a"
dll_sha256="dec964ab1ee36cc9b0ae247d13b376627992fc57dec0454354017ab8fd84f1ea"
providers_sha256="a2b3a50956aa75a9879c8472bc7df4f7a8072bcd2db19a1b7d988e7688f293ef"

if [[ "${1:-}" == "--version" ]]; then
  echo "$ort_version"
  exit 0
fi
if [[ $# -ne 1 || -z "$1" ]]; then
  echo "usage: $0 <DIR> | --version" >&2
  exit 2
fi
dir="$1"

repo="$(cd "$(dirname "$0")/.." && pwd)"
pkg="microsoft.ml.onnxruntime.$ort_version.nupkg"
url="https://api.nuget.org/v3-flatcontainer/microsoft.ml.onnxruntime/$ort_version/$pkg"

if command -v sha256sum >/dev/null 2>&1; then
  sha256() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then
  sha256() { shasum -a 256 "$1" | cut -d' ' -f1; }
else
  echo "sha256sum or shasum is required to verify the download" >&2
  exit 1
fi

has() { [[ -f "$dir/$1" && "$(sha256 "$dir/$1")" == "$2" ]]; }
if has onnxruntime.dll "$dll_sha256" && has onnxruntime_providers_shared.dll "$providers_sha256"; then
  echo "ok      $dir (ONNX Runtime $ort_version)"
  exit 0
fi

work="$(mktemp -d "${TMPDIR:-/tmp}/mam-onnxruntime-XXXXXX")"
trap 'rm -rf "$work"' EXIT

echo "fetch   $url"
curl -fsSL --proto '=https' --tlsv1.2 --retry 3 --connect-timeout 10 --max-time 900 -o "$work/$pkg" "$url"
if [[ "$(sha256 "$work/$pkg")" != "$pkg_sha256" ]]; then
  echo "checksum mismatch: $pkg" >&2
  exit 1
fi

# Only what is used (the package also holds the other platforms, import libraries and headers), by the full names of the members.
native_dir="runtimes/win-x64/native"
members=("$native_dir/onnxruntime.dll" "$native_dir/onnxruntime_providers_shared.dll" "LICENSE" "ThirdPartyNotices.txt")
mkdir "$work/x"
# A path for a program that is not of the shell (Git Bash on Windows: 7z.exe and Windows' own tar.exe do not know /tmp/...).
native() { if command -v cygpath >/dev/null 2>&1; then cygpath -m "$1"; else printf '%s' "$1"; fi; }
if command -v unzip >/dev/null 2>&1; then
  unzip -q "$work/$pkg" "${members[@]}" -d "$work/x"
elif command -v bsdtar >/dev/null 2>&1; then
  bsdtar -xf "$work/$pkg" -C "$work/x" "${members[@]}"
elif tar --version 2>/dev/null | grep -q bsdtar; then
  tar -xf "$work/$pkg" -C "$work/x" "${members[@]}"
elif command -v 7z >/dev/null 2>&1; then
  7z x -y -bso0 -bsp0 "-o$(native "$work/x")" "$(native "$work/$pkg")" "${members[@]}"
elif [[ -x "${SYSTEMROOT:-}/System32/tar.exe" ]]; then
  # Git Bash's tar is GNU tar, which cannot open a zip; the tar.exe of Windows 10 and later is bsdtar.
  "$SYSTEMROOT/System32/tar.exe" -xf "$(native "$work/$pkg")" -C "$(native "$work/x")" "${members[@]}"
else
  echo "unzip, bsdtar (tar.exe) or 7z is required to open the .nupkg" >&2
  exit 1
fi

# The files are the ones the pinned package hash promises, but the check names a wrong pin at once. The license texts must be the ones
# committed (the line ends are LF in the repository: .gitattributes): THIRD_PARTY_NOTICES.md is generated from those copies, so a
# new release with other texts must not be bundled before they are replaced.
[[ "$(sha256 "$work/x/$native_dir/onnxruntime.dll")" == "$dll_sha256" ]] || { echo "unexpected onnxruntime.dll in $pkg" >&2; exit 1; }
[[ "$(sha256 "$work/x/$native_dir/onnxruntime_providers_shared.dll")" == "$providers_sha256" ]] || { echo "unexpected onnxruntime_providers_shared.dll in $pkg" >&2; exit 1; }
for f in LICENSE ThirdPartyNotices.txt; do
  if ! tr -d '\r' <"$work/x/$f" | cmp -s - "$repo/build/windows/onnxruntime/$f"; then
    echo "$f of $pkg differs from build/windows/onnxruntime/$f: replace the committed copy and run scripts/third-party-notices.sh" >&2
    exit 1
  fi
done

mkdir -p "$dir"
cp "$work/x/$native_dir/onnxruntime.dll" "$work/x/$native_dir/onnxruntime_providers_shared.dll" "$dir/"
echo "ONNX Runtime $ort_version is in $dir"
