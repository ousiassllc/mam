package desktop

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// macInstallScript is the helper the macOS update runs, in a process of its own, once the application has
// stopped: /bin/sh -c <script> mam-update <pid> <dmg> <app bundle>. It
//
//  1. waits for process <pid> (the application that is quitting) to end,
//  2. mounts the verified dmg read-only and invisible (hdiutil attach -nobrowse -readonly),
//  3. copies MAM.app from it next to the installed bundle (the same directory, so the swap is a rename on one
//     volume), removes the quarantine attribute (when there is one) and swaps it for the running bundle (the old one is put back when the
//     swap fails),
//  4. unmounts the dmg and starts the new bundle (open).
//
// Any failure leaves the installed bundle as it was, starts it again and says what went wrong in a dialog
// (osascript). The paths come in as arguments, never spliced into the script. The script is POSIX sh: /bin/sh of macOS
// is bash 3.2 in sh mode.
const macInstallScript = `
pid=$1
dmg=$2
app=$3
work=$(dirname "$dmg")
exec >"$work/update.log" 2>&1
mnt=
new=
old=

cleanup() {
  [ -n "$new" ] && rm -rf "$new"
  if [ -n "$mnt" ]; then
    hdiutil detach "$mnt" -quiet || hdiutil detach "$mnt" -force -quiet
    rmdir "$mnt"
  fi
  mnt=
  new=
}

fail() {
  echo "mam update failed: $1"
  cleanup
  osascript -e 'on run argv' \
    -e 'display alert "MAM のアップデートに失敗しました" message (item 1 of argv) as critical' \
    -e 'end run' "$1" >/dev/null 2>&1
  [ -d "$app" ] && open "$app"
  exit 1
}

n=0
while kill -0 "$pid" 2>/dev/null; do
  n=$((n + 1))
  [ "$n" -gt 600 ] && fail "MAM が終了しませんでした。いったん終了してから、もう一度アップデートを確認してください。"
  sleep 0.1
done

mnt=$(mktemp -d "$work/mnt.XXXXXX") || fail "作業用のフォルダを作れませんでした。"
hdiutil attach -nobrowse -readonly -noverify -noautoopen -mountpoint "$mnt" "$dmg" || {
  rmdir "$mnt"
  mnt=
  fail "ダウンロードしたディスクイメージ（dmg）を開けませんでした。"
}
src="$mnt/MAM.app"
[ -x "$src/Contents/MacOS/mam" ] || fail "ディスクイメージの中に MAM.app が見つかりません。"

parent=$(dirname "$app")
new="$parent/.mam-update-new.$$"
old="$parent/.mam-update-old.$$"
rm -rf "$new" "$old"
ditto "$src" "$new" || fail "新しい MAM.app をコピーできませんでした（$parent に書き込めません）。"
# A dmg the updater downloaded carries no quarantine attribute, and xattr -d reports a missing one as an error:
# that is not a reason to give up the update.
xattr -dr com.apple.quarantine "$new" || echo "mam update: no quarantine attribute removed from $new"
mv "$app" "$old" || fail "いまの MAM.app を退避できませんでした。"
if ! mv "$new" "$app"; then
  mv "$old" "$app"
  fail "新しい MAM.app に入れ替えられませんでした。"
fi
rm -rf "$old"
new=
cleanup
open "$app"
`

// MacInstallCommand builds, without starting it, the command that installs the verified dmg over appBundle once
// the process pid (this application) has ended and starts the new version (macInstallScript). It is a function of
// its own because mounting a dmg cannot be tested on the Linux CI: what is tested is the command that is built, and
// the script itself against stand-ins for the macOS tools.
func MacInstallCommand(pid int, dmgPath, appBundle string) *exec.Cmd {
	// No context: the helper must outlive this process, so nothing may ever kill it.
	cmd := exec.Command("/bin/sh", "-c", macInstallScript, "mam-update", strconv.Itoa(pid), dmgPath, appBundle) // #nosec G204 -- fixed script; the dmg the updater downloaded and verified and this app's own bundle
	detach(cmd)
	return cmd
}

// MacAppBundle is the .app bundle that the executable exe (the resolved path of os.Executable) runs from:
// <name>.app/Contents/MacOS/<exe>.
func MacAppBundle(exe string) (string, error) {
	macOSDir := filepath.Dir(exe)
	contents := filepath.Dir(macOSDir)
	bundle := filepath.Dir(contents)
	if filepath.Base(macOSDir) != "MacOS" || filepath.Base(contents) != "Contents" || !strings.HasSuffix(bundle, ".app") {
		return "", fmt.Errorf("%s is not inside a .app bundle (<name>.app/Contents/MacOS)", exe)
	}
	return bundle, nil
}

// MacUpdatableBundle is MacAppBundle, refused when the bundle cannot be replaced where it is: it runs from a
// mounted disk image (/Volumes/..., read-only) or from the randomized read-only copy macOS makes of an app that was
// downloaded and opened without being moved (App Translocation), or its directory is not writable by this user.
// The user moves the app to /Applications (or any folder they own) first.
func MacUpdatableBundle(exe string) (string, error) {
	bundle, err := MacAppBundle(exe)
	if err != nil {
		return "", err
	}
	if strings.Contains(bundle, "/AppTranslocation/") || strings.HasPrefix(bundle, "/Volumes/") {
		return "", fmt.Errorf("%s is running from a disk image or a translocated copy: move MAM to /Applications and start it from there", bundle)
	}
	probe, err := os.CreateTemp(filepath.Dir(bundle), ".mam-update-probe-*")
	if err != nil {
		return "", fmt.Errorf("the folder of %s is not writable: %w", bundle, err)
	}
	return bundle, errors.Join(probe.Close(), os.Remove(probe.Name()))
}
