#!/usr/bin/env bash
# Makes `makensis` available for scripts/package-windows.sh on the self-hosted Linux runner (release.yml, `build` job).
# The installer is made by makensis (NSIS). Install it when it is missing, without needing root:
#   1. makensis on PATH: use it.
#   2. passwordless sudo: apt-get install nsis.
#   3. otherwise: apt-get download nsis + nsis-common (no root needed) and dpkg -x them into a per-user cache,
#      with a makensis wrapper that sets NSISDIR (the stubs live in nsis-common). The cache survives between runs.
# The version is the distro's (not pinned). In GitHub Actions the wrapper's directory is added to $GITHUB_PATH.
set -euo pipefail

if ! command -v makensis >/dev/null; then
  if sudo -n true 2>/dev/null; then
    sudo -n apt-get update && sudo -n apt-get install -y --no-install-recommends nsis
  else
    nsis_home="${XDG_CACHE_HOME:-$HOME/.cache}/mam-release/nsis"
    if [[ ! -x "$nsis_home/bin/makensis" ]]; then
      tmp="$(mktemp -d)"
      (cd "$tmp" && apt-get download nsis nsis-common && for deb in ./*.deb; do dpkg -x "$deb" root; done)
      rm -rf "$nsis_home"
      mkdir -p "$nsis_home/bin"
      mv "$tmp/root" "$nsis_home/root"
      rm -rf "$tmp"
      printf '#!/bin/sh\nNSISDIR=%s exec %s "$@"\n' \
        "$nsis_home/root/usr/share/nsis" "$nsis_home/root/usr/bin/makensis" >"$nsis_home/bin/makensis"
      chmod +x "$nsis_home/bin/makensis"
    fi
    echo "$nsis_home/bin" >>"${GITHUB_PATH:-/dev/null}"
    export PATH="$nsis_home/bin:$PATH"
  fi
fi
command -v makensis >/dev/null || { echo "::error::makensis is missing and could not be installed"; exit 1; }
makensis -VERSION
