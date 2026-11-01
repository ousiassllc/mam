#!/usr/bin/env bash
# Fails when THIRD_PARTY_NOTICES.md of the repository root is not what scripts/third-party-notices.sh writes now. The file is the one
# the installer ships and the one the binary embeds (notices.go; the settings screen "第三者のライセンス" shows it), so it must never
# be left behind when go.mod, static/package-lock.json, the ONNX Runtime texts or scripts/third-party-notices-*.md change.
# Usage: scripts/check-third-party-notices.sh      Needs what the generator needs: go, node and `npm ci --prefix static` done.
set -euo pipefail

cd "$(dirname "$0")/.."
tmp="$(mktemp "${TMPDIR:-/tmp}/mam-notices-check-XXXXXX")"
trap 'rm -f "$tmp"' EXIT
bash scripts/third-party-notices.sh "$tmp" 2>/dev/null
if ! cmp -s "$tmp" THIRD_PARTY_NOTICES.md; then
  echo "THIRD_PARTY_NOTICES.md is out of date: run scripts/third-party-notices.sh and commit it" >&2
  diff -u THIRD_PARTY_NOTICES.md "$tmp" | head -n 40 >&2 || true
  exit 1
fi
echo "ok: THIRD_PARTY_NOTICES.md is what scripts/third-party-notices.sh writes"
