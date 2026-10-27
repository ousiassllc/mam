#!/usr/bin/env bash
# macOS desktop packaging (Apple Silicon, unsigned): templ generate -> esbuild + tailwind (minify) -> go build of the Wails
# window front backend/cmd/wails (cgo) -> MAM.app -> ad-hoc signature -> .dmg (hdiutil). Run on a Mac with Apple Silicon (the
# GitHub-hosted macos-latest runner: release.yml's `build-macos` job and ci.yml's `macos` job); spec/architecture/desktop.md §4,
# spec/environment/setup.md "リリース".
#
# Usage: scripts/package-macos.sh <version>      <version> is X.Y.Z (the tag v<version> without the v)
# Output: bin/mam-<version>-darwin-arm64.dmg      MAM.app next to an Applications link, volume name MAM
# The binary embeds backend/internal/version.Version = "v<version>" (the auto update compares it with the latest release) and
# version.ReleasePublicKey = $RELEASE_SIGNING_PUBLIC_KEY (base64 of the raw 32-byte ed25519 public key that verifies
# checksums.txt.sig; optional: empty = not embedded, the app then skips the signature check with a warning), exactly like
# scripts/package-windows.sh.
# NO Apple Developer ID signature and NO notarization: the bundle only gets the ad-hoc signature (`codesign --sign -`) that
# an arm64 binary needs to run at all. Gatekeeper therefore asks the user on the first launch (README.md).
# Needs (nothing is installed here): Xcode command line tools (preinstalled on macos-latest; clang for cgo; sips, iconutil, plutil,
# codesign, ditto, hdiutil, lipo ship with macOS), go, node/npm.
# templ is not needed on PATH: it runs through `go run` at the version of go.mod's github.com/a-h/templ (like `make generate`).
# Scratch files live in a fresh directory under $RUNNER_TEMP (else $TMPDIR) that is removed on exit.
set -euo pipefail

if [[ $# -ne 1 || ! "$1" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "usage: $0 <version>   (version must be X.Y.Z, e.g. 1.2.3)" >&2
  exit 2
fi
version="$1"

if [[ "$(uname -s)" != Darwin || "$(uname -m)" != arm64 ]]; then
  echo "package-macos.sh builds the Apple Silicon app and must run on a Mac with Apple Silicon (this is $(uname -s) $(uname -m))" >&2
  exit 1
fi

cd "$(dirname "$0")/.."

# Without the Xcode command line tools `clang` is only a stub that opens an installer dialog: ask xcode-select instead.
xcode-select -p >/dev/null 2>&1 || { echo "the Xcode command line tools are not installed (xcode-select --install): cgo needs them" >&2; exit 1; }
for tool in go npm clang sips iconutil plutil codesign ditto hdiutil lipo; do
  command -v "$tool" >/dev/null || { echo "$tool not found in PATH" >&2; exit 1; }
done

export RELEASE_SIGNING_PUBLIC_KEY="${RELEASE_SIGNING_PUBLIC_KEY:-}"
if [[ -z "$RELEASE_SIGNING_PUBLIC_KEY" ]]; then
  echo "warning: RELEASE_SIGNING_PUBLIC_KEY is empty: the app is built WITHOUT the release public key (auto update signature check skipped)" >&2
fi

# Never the templ that is first on PATH: a stale binary rewrites the committed *_templ.go differently from go.mod's runtime
# (the same as scripts/package-windows.sh and the Makefile's TEMPL_RUN).
echo "==> templ generate"
templ_version="$(go list -m -f '{{.Version}}' github.com/a-h/templ)"
go run github.com/a-h/templ/cmd/templ@"$templ_version" generate ./backend/...

echo "==> esbuild + tailwind (minify)"
npm ci --prefix static
npm run build --prefix static -- --minify

work_root="${RUNNER_TEMP:-${TMPDIR:-/tmp}}"
stage="$(mktemp -d "${work_root%/}/mam-macos-XXXXXX")"
trap 'rm -rf "$stage"' EXIT
app="$stage/MAM.app"
dmg="bin/mam-$version-darwin-arm64.dmg"
mkdir -p bin "$app/Contents/MacOS" "$app/Contents/Resources"
rm -f bin/mam-*-darwin-arm64.dmg

echo "==> icons.icns (from build/appicon.png)"
iconset="$stage/icons.iconset"
mkdir "$iconset"
for size in 16 32 128 256 512; do
  sips -z "$size" "$size" build/appicon.png --out "$iconset/icon_${size}x${size}.png" >/dev/null
  sips -z "$((size * 2))" "$((size * 2))" build/appicon.png --out "$iconset/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns "$iconset" -o "$app/Contents/Resources/icons.icns"

sed "s/@VERSION@/$version/g" build/darwin/Info.plist >"$app/Contents/Info.plist"
plutil -lint "$app/Contents/Info.plist"
cp LICENSE THIRD_PARTY_NOTICES.md "$app/Contents/Resources/"

# cgo is required on macOS (Wails uses Cocoa and WebKit). The deployment target matches LSMinimumSystemVersion.
echo "==> go build (darwin/arm64, cgo)"
CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 \
  CGO_CFLAGS="-mmacosx-version-min=12.0" CGO_LDFLAGS="-mmacosx-version-min=12.0" MACOSX_DEPLOYMENT_TARGET="12.0" \
  go build -tags production -trimpath -buildvcs=false \
  -ldflags="-s -w -X github.com/ousiassllc/mam/backend/internal/version.Version=v$version -X github.com/ousiassllc/mam/backend/internal/version.ReleasePublicKey=$RELEASE_SIGNING_PUBLIC_KEY" \
  -o "$app/Contents/MacOS/mam" ./backend/cmd/wails
[[ "$(lipo -archs "$app/Contents/MacOS/mam")" == arm64 ]] || { echo "the binary is not arm64 only" >&2; exit 1; }

# An arm64 binary must carry at least an ad-hoc signature to be allowed to run; sign the whole bundle so that it is consistent.
echo "==> ad-hoc codesign"
xattr -cr "$app"
codesign --force --deep --sign - "$app"
codesign --verify --deep --strict "$app"

# The binary must at least load (dyld, frameworks): --detect-gpu prints a report and exits without a window or a data directory.
echo "==> smoke: mam --detect-gpu"
"$app/Contents/MacOS/mam" --detect-gpu >/dev/null

echo "==> dmg"
dmg_root="$stage/dmg"
mkdir "$dmg_root"
ditto "$app" "$dmg_root/MAM.app"
ln -s /Applications "$dmg_root/Applications"
hdiutil create -volname MAM -srcfolder "$dmg_root" -fs HFS+ -format UDZO -ov "$dmg"
[[ -f "$dmg" ]] || { echo "dmg was not created: $dmg" >&2; exit 1; }
echo "built $dmg"
