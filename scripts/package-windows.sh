#!/usr/bin/env bash
# Windows desktop packaging: templ generate -> esbuild + tailwind (minify) -> `wails3 package GOOS=windows`
# (Wails window front backend/cmd/wails + NSIS installer; spec/architecture/desktop.md §4).
#
# Usage: scripts/package-windows.sh <version>      <version> is X.Y.Z (the tag v<version> without the v)
# Output:
#   bin/mam-<version>-windows-amd64.exe             portable mam.exe (Wails window front)
#   bin/mam-<version>-windows-amd64-setup.exe       per-user NSIS installer ($LOCALAPPDATA\Programs\MAM, no administrator
#                                                   rights; includes the WebView2 bootstrapper and, next to mam.exe, the ONNX
#                                                   Runtime DLLs of the Face Identity Similarity)
# The exe embeds backend/internal/version.Version = "v<version>" (the auto update compares it with the latest release) and
# version.ReleasePublicKey = $RELEASE_SIGNING_PUBLIC_KEY (base64 of the raw 32-byte ed25519 public key that verifies
# checksums.txt.sig; optional: empty = not embedded, the app then skips the signature check with a warning).
# The portable exe is the one file: it has no ONNX Runtime DLL (the face numbers are then off until onnxruntime.dll lies next to it).
# Needs: go, node/npm, wails3 (the version of go.mod's github.com/wailsapp/wails/v3), makensis, jq, curl, unzip (or bsdtar / 7z).
# templ is not needed on PATH: it runs through `go run` at the version of go.mod's github.com/a-h/templ (like `make generate`).
set -euo pipefail

if [[ $# -ne 1 || ! "$1" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "usage: $0 <version>   (version must be X.Y.Z, e.g. 1.2.3)" >&2
  exit 2
fi
version="$1"

cd "$(dirname "$0")/.."

for tool in go npm wails3 makensis jq; do
  command -v "$tool" >/dev/null || { echo "$tool not found in PATH" >&2; exit 1; }
done

# The Wails CLI and the library must be the same version (the CLI generates the resources and runs the build).
want="$(go list -m -f '{{.Version}}' github.com/wailsapp/wails/v3)"
have="$(wails3 version 2>&1 | tr -d '[:space:]')"
if [[ "$have" != "$want" ]]; then
  echo "wails3 $have does not match go.mod ($want): go install github.com/wailsapp/wails/v3/cmd/wails3@$want" >&2
  exit 1
fi

# The Taskfile reads RELEASE_SIGNING_PUBLIC_KEY from the environment for the -X ldflags.
export RELEASE_SIGNING_PUBLIC_KEY="${RELEASE_SIGNING_PUBLIC_KEY:-}"
if [[ -z "$RELEASE_SIGNING_PUBLIC_KEY" ]]; then
  echo "warning: RELEASE_SIGNING_PUBLIC_KEY is empty: the exe is built WITHOUT the release public key (auto update signature check skipped)" >&2
fi

# Never the templ that is first on PATH: a stale binary rewrites the committed *_templ.go differently from go.mod's runtime
# (Windows has no make, so this repeats the Makefile's TEMPL_RUN).
echo "==> templ generate"
templ_version="$(go list -m -f '{{.Version}}' github.com/a-h/templ)"
go run github.com/a-h/templ/cmd/templ@"$templ_version" generate ./backend/...

echo "==> esbuild + tailwind (minify)"
npm ci --prefix static
npm run build --prefix static -- --minify

# ONNX Runtime (MIT) for the Face Identity Similarity: the pinned release is downloaded over https and checked against its SHA-256
# (scripts/fetch-onnxruntime.sh), then build/windows/nsis/project.nsi puts the two DLLs next to mam.exe. Never fetched by the app at run time.
echo "==> ONNX Runtime DLLs"
bash scripts/fetch-onnxruntime.sh build/windows/nsis/onnxruntime

# The version goes into the exe resources through a copy of info.json (the committed one stays 0.0.0).
info_json="$(mktemp "${TMPDIR:-/tmp}/mam-info-XXXXXX.json")"
trap 'rm -f "$info_json"' EXIT
jq --arg v "$version" '.fixed.file_version = $v | .info["0000"].ProductVersion = $v' build/windows/info.json >"$info_json"
# wails3 generate syso runs in build/, so the path must be absolute.
info_json_abs="$(cd "$(dirname "$info_json")" && pwd)/$(basename "$info_json")"
case "$(uname -s)" in MINGW* | MSYS* | CYGWIN*) info_json_abs="$(cygpath -m "$info_json_abs")" ;; esac

portable="bin/mam-$version-windows-amd64.exe"
installer="bin/mam-$version-windows-amd64-setup.exe"
mkdir -p bin
rm -f bin/mam.exe "$portable" "$installer"

echo "==> wails3 package GOOS=windows (amd64)"
GOARCH=amd64 wails3 package GOOS=windows "VERSION=$version" "INFO_JSON=$info_json_abs"

# bin/mam.exe is the installed name (the Start menu shortcut and the installer use it); the release
# carries it as the portable exe under its versioned name.
mv bin/mam.exe "$portable"
[[ -f "$installer" ]] || { echo "installer was not created: $installer" >&2; exit 1; }
echo "built $portable"
echo "built $installer"
