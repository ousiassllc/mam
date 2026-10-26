#!/usr/bin/env bash
# Windows installer smoke test (run by hand on a Windows machine, in Git Bash; the release workflow runs on Linux and does not run it):
# silently install the per-user NSIS installer into its default place ($LOCALAPPDATA\Programs\MAM, no administrator rights,
# no UAC prompt: the same way the auto update runs it), check what it should have created (files, HKCU uninstall key,
# per-user shortcuts), start the installed mam.exe and require GET / to answer 200 from the 127.0.0.1 server, then
# uninstall silently. An existing installation in that place is replaced, then removed at the end: run it on a test machine.
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
install_dir_win="$LOCALAPPDATA\\Programs\\MAM"
install_dir="$(cygpath -u "$install_dir_win")"

# A running mam.exe locks the files of the install directory (the installer may also relaunch the app after a silent install).
stop_mam() {
  taskkill //F //IM mam.exe >/dev/null 2>&1 || true
}

# The installer is per-user: a plain `/S` without /D installs to $LOCALAPPDATA\Programs\MAM.
stop_mam
echo "==> silent install (per-user) to $install_dir_win"
powershell.exe -NoProfile -Command "\$p = Start-Process -FilePath '$installer' -ArgumentList '/S' -Wait -PassThru; exit \$p.ExitCode"
# A relaunch after the install would hold the default data dir and port: stop it, the smoke starts its own instance.
sleep 3
stop_mam

echo "==> installed files"
for f in "$install_dir/mam.exe" "$install_dir/uninstall.exe"; do
  [[ -f "$f" ]] || { echo "missing: $f" >&2; exit 1; }
  echo "ok: $f"
done
# Per-user scope: the uninstall entry is in HKCU (not HKLM) and the shortcuts are in the current user's Start menu and desktop.
# (the key name is <company><product> = ousiassllcMAM, see UNINST_KEY_NAME in wails_tools.nsh)
if reg.exe query 'HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\ousiassllcMAM' >/dev/null 2>&1; then
  echo "ok: HKCU uninstall key"
else
  echo "missing: HKCU uninstall key (the installer is not per-user?)" >&2
  exit 1
fi
for f in "$(cygpath -u "$APPDATA")/Microsoft/Windows/Start Menu/Programs/MAM.lnk" "$(cygpath -u "$USERPROFILE")/Desktop/MAM.lnk"; do
  if [[ -f "$f" ]]; then echo "ok: $f"; else echo "note: no shortcut at $f (the desktop may be redirected)"; fi
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
  stop_mam
fi

# _?= keeps the uninstaller in place so that -Wait waits for the removal (it otherwise copies itself to %TEMP% and returns).
echo "==> silent uninstall"
uninstaller="$(cygpath -w "$install_dir/uninstall.exe")"
powershell.exe -NoProfile -Command "\$p = Start-Process -FilePath '$uninstaller' -ArgumentList '/S _?=$install_dir_win' -Wait -PassThru; exit \$p.ExitCode" || echo "uninstall exited non-zero" >&2
if [[ -f "$install_dir/mam.exe" ]]; then echo "note: $install_dir/mam.exe is still there after the uninstall" >&2; fi

for log in "$(cygpath -u "$data_dir")"/logs/*; do
  [[ -f "$log" ]] && { echo "--- $log"; tail -n 20 "$log"; }
done

[[ "$status" == "200" ]] || { echo "smoke test failed" >&2; exit 1; }
echo "smoke test passed"
