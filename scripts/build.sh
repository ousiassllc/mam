#!/usr/bin/env bash
# Production build: templ generate -> esbuild (minify) -> tailwind (minify) -> go build (go:embed).
# Output: bin/mam[.exe]. Cross-compile with GOOS/GOARCH (pure Go, CGO not required).
set -euo pipefail

cd "$(dirname "$0")/.."

out="bin/mam"
[[ "${GOOS:-$(go env GOOS)}" == "windows" ]] && out="bin/mam.exe"

echo "==> templ generate"
templ generate ./backend/...

echo "==> esbuild + tailwind (minify)"
npm ci --prefix static
npm run build --prefix static -- --minify

echo "==> go build"
mkdir -p bin
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$out" ./backend/cmd/server
echo "built $out"
