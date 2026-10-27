#!/usr/bin/env bash
# Writes <dir>/checksums.txt for a release (release.yml, `sign` job): `sha256sum` lines (`<sha256>  <file name>`) of the
# two installers the apps update themselves with: the Windows installer and the macOS dmg. The portable exe is not
# listed (it is never installed automatically). Both files must exist: a release that lacks one is not published.
#
# Usage: scripts/make-checksums.sh <dir> <version>      <version> is X.Y.Z (the tag v<version> without the v)
set -euo pipefail

if [[ $# -ne 2 || ! -d "$1" || ! "$2" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "usage: $0 <dir containing the installers> <version>   (version must be X.Y.Z, e.g. 1.2.3)" >&2
  exit 2
fi
dir="$1"
version="$2"

files=("mam-$version-windows-amd64-setup.exe" "mam-$version-darwin-arm64.dmg")
for f in "${files[@]}"; do
  [[ -f "$dir/$f" ]] || { echo "::error::$f is missing in $dir: the release needs both installers" >&2; exit 1; }
done

(cd "$dir" && sha256sum -- "${files[@]}" >checksums.txt)
cat "$dir/checksums.txt"
