#!/usr/bin/env bash
# Production build: templ generate -> esbuild (minify) -> tailwind (minify) -> go build (go:embed).
# The front-end is built once; the Go binary is pure Go (CGO not required), so it cross-compiles.
#
# Usage:
#   scripts/build.sh          one binary for the host (or for GOOS/GOARCH): bin/mam[.exe]
#   scripts/build.sh --all    every supported platform: bin/mam-<os>-<arch>[.exe]
#                             (windows, darwin, linux x amd64, arm64; spec/requirements/non-functional.md §1)
set -euo pipefail

cd "$(dirname "$0")/.."

platforms=(windows/amd64 windows/arm64 darwin/amd64 darwin/arm64 linux/amd64 linux/arm64)
case "${1:-}" in
  "") targets=("${GOOS:-$(go env GOOS)}/${GOARCH:-$(go env GOARCH)}"); single=1 ;;
  --all) targets=("${platforms[@]}"); single=0 ;;
  *) echo "usage: $0 [--all]" >&2; exit 2 ;;
esac

echo "==> templ generate"
make --no-print-directory generate

echo "==> esbuild + tailwind (minify)"
npm ci --prefix static
npm run build --prefix static -- --minify

echo "==> go build"
mkdir -p bin
for target in "${targets[@]}"; do
  os="${target%/*}" arch="${target#*/}"
  ext=""
  [[ "$os" == "windows" ]] && ext=".exe"
  if [[ "$single" == 1 ]]; then out="bin/mam$ext"; else out="bin/mam-$os-$arch$ext"; fi
  GOOS="$os" GOARCH="$arch" CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$out" ./backend/cmd/server
  echo "built $out"
done
