#!/usr/bin/env bash
# Windows installer smoke test (run by hand on a Windows machine, in Git Bash; the release workflow runs on Linux and does not run it):
# silently install the NSIS installer, check what it should have created, start the installed
# mam.exe and require GET / to answer 200 from the 127.0.0.1 server.
#
# Usage: scripts/smoke-windows.sh <path to mam-<version>-windows-amd64-setup.exe>
set -euo pipefail

if [[ $# -ne 1 || ! -f "$1" ]]; then
  echo "usage: $0 <installer.exe>" >&2
  exit 2
fi
installer="$(cygpath -w "$(realpath "$1")")"

port=18787
work="${RUNNER_TEMP:-${TMPDIR:-/tmp}}"
data_dir="$(cygpath -w "$work/mam-smoke-data")"
install_dir_win="$(cygpath -w "$work/mam-smoke-install")"
install_dir="$(cygpath -u "$install_dir_win")"

# /D= must be the last argument and is not quoted (NSIS).
echo "==> silent install to $install_dir_win"
powershell.exe -NoProfile -Command "\$p = Start-Process -FilePath '$installer' -ArgumentList '/S /D=$install_dir_win' -Wait -PassThru; exit \$p.ExitCode"

echo "==> installed files"
for f in "$install_dir/mam.exe" "$install_dir/uninstall.exe"; do
  [[ -f "$f" ]] || { echo "missing: $f" >&2; exit 1; }
  echo "ok: $f"
done
# The shortcuts are informational: they are in the all-users Start menu and desktop.
for f in "$(cygpath -u "$PROGRAMDATA")/Microsoft/Windows/Start Menu/Programs/MAM.lnk" "$(cygpath -u "$PUBLIC")/Desktop/MAM.lnk"; do
  if [[ -f "$f" ]]; then echo "ok: $f"; else echo "note: no shortcut at $f"; fi
done

echo "==> start mam.exe"
"$install_dir/mam.exe" --data-dir "$data_dir" --port "$port" &
pid=$!

status=""
for _ in $(seq 1 60); do
  status="$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$port/" || true)"
  [[ "$status" == "200" ]] && break
  if ! kill -0 "$pid" 2>/dev/null; then
    echo "mam.exe exited before it answered" >&2
    break
  fi
  sleep 1
done

echo "GET / -> ${status:-none}"
# Stop it: a close request first (the window front shuts the server down gracefully), then force.
taskkill //IM mam.exe >/dev/null 2>&1 || true
for _ in $(seq 1 15); do kill -0 "$pid" 2>/dev/null || break; sleep 1; done
if kill -0 "$pid" 2>/dev/null; then
  echo "mam.exe did not exit after a close request; forcing" >&2
  taskkill //F //IM mam.exe >/dev/null 2>&1 || true
fi

for log in "$(cygpath -u "$data_dir")"/logs/*; do
  [[ -f "$log" ]] && { echo "--- $log"; tail -n 20 "$log"; }
done

[[ "$status" == "200" ]] || { echo "smoke test failed" >&2; exit 1; }
echo "smoke test passed"
