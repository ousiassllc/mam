//go:build !windows

package desktop

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMacInstallCommand_RunsTheScriptWithTheArgumentsOutsideTheScriptText(t *testing.T) {
	const (
		dmg = "/var/folders/x/mam-update-123/mam-0.2.0-darwin-arm64.dmg"
		app = "/Applications/MAM $(touch pwned).app"
	)
	cmd := MacInstallCommand(4242, dmg, app)

	assert.Equal(t, []string{"/bin/sh", "-c", macInstallScript, "mam-update", "4242", dmg, app}, cmd.Args)
	assert.Nil(t, cmd.Cancel, "no context: nothing may kill the helper when this process ends")
	assert.NotContains(t, macInstallScript, "pwned", "paths are arguments, never part of the script")
	require.NotNil(t, cmd.SysProcAttr)
	assert.True(t, cmd.SysProcAttr.Setsid, "the helper has a session of its own, so the end of this process does not reach it")
}

func TestMacAppBundle(t *testing.T) {
	tests := []struct {
		exe     string
		want    string
		wantErr bool
	}{
		{exe: "/Applications/MAM.app/Contents/MacOS/mam", want: "/Applications/MAM.app"},
		{exe: "/Users/u/Apps/Other Name.app/Contents/MacOS/mam", want: "/Users/u/Apps/Other Name.app"},
		{exe: "/usr/local/bin/mam", wantErr: true},
		{exe: "/Applications/MAM.app/Contents/Resources/mam", wantErr: true},
		{exe: "/Applications/MAM/Contents/MacOS/mam", wantErr: true},
		{exe: "mam", wantErr: true},
	}
	for _, tt := range tests {
		got, err := MacAppBundle(tt.exe)
		if tt.wantErr {
			require.Error(t, err, tt.exe)
			continue
		}
		require.NoError(t, err, tt.exe)
		assert.Equal(t, tt.want, got)
	}
}

func TestMacUpdatableBundle(t *testing.T) {
	// Running from a disk image or a translocated copy: refused before the file system is touched.
	for _, exe := range []string{
		"/Volumes/MAM/MAM.app/Contents/MacOS/mam",
		"/private/var/folders/ab/c/T/AppTranslocation/0E3A/d/MAM.app/Contents/MacOS/mam",
	} {
		_, err := MacUpdatableBundle(exe)
		require.Error(t, err, exe)
		assert.Contains(t, err.Error(), "/Applications", "the message tells where to move the app")
	}

	// A bundle in a folder the user can write to.
	dir := t.TempDir()
	exe := filepath.Join(dir, "MAM.app", "Contents", "MacOS", "mam")
	require.NoError(t, os.MkdirAll(filepath.Dir(exe), 0o750))
	got, err := MacUpdatableBundle(exe)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "MAM.app"), got)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "the writability probe leaves nothing behind")

	// Not inside a bundle at all.
	_, err = MacUpdatableBundle(filepath.Join(dir, "mam"))
	require.Error(t, err)

	// A folder that is not writable (root ignores the mode).
	if os.Geteuid() != 0 {
		require.NoError(t, os.Chmod(dir, 0o500))       // #nosec G302 -- the test makes the folder read-only
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) // #nosec G302 -- so TempDir can remove it
		_, err = MacUpdatableBundle(exe)
		require.Error(t, err)
	}
}

func TestUpdater_RunPendingReturnsTheFailureToBuildTheCommand(t *testing.T) {
	u := NewUpdater(func() {})
	boom := os.ErrPermission
	u.command = func(string) (*exec.Cmd, error) { return nil, boom }
	u.start = func(*exec.Cmd) error { t.Fatal("started without a command"); return nil }
	u.Install("/tmp/mam.dmg")

	require.ErrorIs(t, u.RunPending(), boom)
}

func TestInstallCommandFor_OffMacIsTheSilentInstaller(t *testing.T) {
	cmd, err := installCommandFor("windows", `C:\t\setup.exe`)
	require.NoError(t, err)
	assert.Equal(t, []string{`C:\t\setup.exe`, "/S"}, cmd.Args)
}

// The helper script runs for real here, against stand-ins for the macOS tools (hdiutil, ditto, xattr, open,
// osascript) that record their calls and mount a fixture bundle: what is checked is the order of the steps and what is
// left on disk when one of them fails. Mounting a real dmg is not possible on Linux (spec/architecture/desktop.md §6.6).
type macScriptEnv struct {
	t       *testing.T
	root    string // temp dir holding everything below
	apps    string // the folder of the installed bundle
	app     string // the installed bundle
	dmgDir  string // the temp directory the updater downloaded the dmg into
	dmg     string
	fixture string // what the stand-in hdiutil "mounts"
	log     string
	env     []string
}

func newMacScriptEnv(t *testing.T, installedName string) *macScriptEnv {
	t.Helper()
	root := t.TempDir()
	e := &macScriptEnv{t: t, root: root, apps: filepath.Join(root, "Applications"), dmgDir: filepath.Join(root, "mam-update-1"),
		fixture: filepath.Join(root, "fixture"), log: filepath.Join(root, "calls.log")}
	e.app = filepath.Join(e.apps, installedName)
	e.writeExe(filepath.Join(e.app, "Contents", "MacOS", "mam"), "old")
	e.writeExe(filepath.Join(e.fixture, "MAM.app", "Contents", "MacOS", "mam"), "new")
	require.NoError(t, os.MkdirAll(e.dmgDir, 0o750))
	e.dmg = filepath.Join(e.dmgDir, "mam-0.2.0-darwin-arm64.dmg")
	require.NoError(t, os.WriteFile(e.dmg, []byte("dmg"), 0o600))

	bin := filepath.Join(root, "bin")
	require.NoError(t, os.MkdirAll(bin, 0o750))
	stubs := map[string]string{
		// attach ... -mountpoint <dir> <dmg>: "mounts" the fixture at <dir>.
		"hdiutil": `case "$1" in
attach)
  [ -n "$STUB_ATTACH_FAIL" ] && exit 1
  while [ $# -gt 0 ]; do [ "$1" = -mountpoint ] && mp=$2; shift; done
  echo "attach $mp" >>"$STUB_LOG"
  cp -R "$STUB_FIXTURE"/. "$mp"/ ;;
detach) echo "detach $2" >>"$STUB_LOG"; rm -rf "$2"/* ;; # unmounting leaves the empty mount point
esac`,
		"ditto":     `[ -n "$STUB_DITTO_FAIL" ] && exit 1; cp -R "$1" "$2"`,
		"xattr":     `echo "xattr $*" >>"$STUB_LOG"; [ -n "$STUB_XATTR_FAIL" ] && exit 1; exit 0`,
		"open":      `echo "open $*" >>"$STUB_LOG"`,
		"osascript": `for a; do last=$a; done; echo "osascript $last" >>"$STUB_LOG"`,
	}
	for name, body := range stubs {
		e.writeExe(filepath.Join(bin, name), body)
	}
	e.env = []string{"PATH=" + bin + ":/usr/bin:/bin", "STUB_LOG=" + e.log, "STUB_FIXTURE=" + e.fixture, "TMPDIR=" + root}
	return e
}

func (e *macScriptEnv) writeExe(path, body string) {
	e.t.Helper()
	require.NoError(e.t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(e.t, os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o700)) // #nosec G306 -- an executable stand-in
}

// run executes the helper for the process pid, with extra environment, and returns its error.
func (e *macScriptEnv) run(pid int, extraEnv ...string) error {
	cmd := MacInstallCommand(pid, e.dmg, e.app)
	cmd.Env = append(append([]string{}, e.env...), extraEnv...)
	return cmd.Run()
}

func (e *macScriptEnv) calls() []string {
	data, err := os.ReadFile(e.log)
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(e.t, err)
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func (e *macScriptEnv) executable() string {
	data, err := os.ReadFile(filepath.Join(e.app, "Contents", "MacOS", "mam"))
	require.NoError(e.t, err)
	return strings.TrimSpace(strings.TrimPrefix(string(data), "#!/bin/sh"))
}

// leftovers lists what the script must not leave next to the bundle (the staged copy, the old bundle) or in the
// download directory (the mount point).
func (e *macScriptEnv) leftovers() []string {
	var left []string
	apps, err := os.ReadDir(e.apps)
	require.NoError(e.t, err)
	for _, a := range apps {
		if strings.HasPrefix(a.Name(), ".mam-update-") {
			left = append(left, a.Name())
		}
	}
	dl, err := os.ReadDir(e.dmgDir)
	require.NoError(e.t, err)
	for _, d := range dl {
		if strings.HasPrefix(d.Name(), "mnt.") {
			left = append(left, d.Name())
		}
	}
	return left
}

// deadPID is the pid of a process that has ended and been reaped.
func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	require.NoError(t, cmd.Run())
	return cmd.Process.Pid
}

func TestMacInstallScript_ReplacesTheBundleAndStartsTheNewOne(t *testing.T) {
	for _, name := range []string{"MAM.app", "MAM 2.app"} { // the installed bundle may have been renamed
		t.Run(name, func(t *testing.T) {
			e := newMacScriptEnv(t, name)

			require.NoError(t, e.run(deadPID(t)))

			assert.Equal(t, "new", e.executable())
			assert.Empty(t, e.leftovers())
			calls := e.calls()
			require.Len(t, calls, 4, strings.Join(calls, "\n"))
			assert.True(t, strings.HasPrefix(calls[0], "attach "+e.dmgDir+"/mnt."), calls[0])
			assert.True(t, strings.HasPrefix(calls[1], "xattr -dr com.apple.quarantine "+e.apps+"/.mam-update-new."), calls[1])
			assert.Equal(t, "detach "+strings.TrimPrefix(calls[0], "attach "), calls[2])
			assert.Equal(t, "open "+e.app, calls[3], "the new version is started through LaunchServices at the installed path")
		})
	}
}

func TestMacInstallScript_WaitsForTheApplicationToEnd(t *testing.T) {
	e := newMacScriptEnv(t, "MAM.app")
	sleeper := exec.Command("sleep", "0.5")
	require.NoError(t, sleeper.Start())
	go func() { _ = sleeper.Wait() }() // reaped as soon as it ends: a zombie would still answer kill -0

	start := time.Now()
	require.NoError(t, e.run(sleeper.Process.Pid))

	assert.GreaterOrEqual(t, time.Since(start), 300*time.Millisecond, "the helper did not wait for pid "+strconv.Itoa(sleeper.Process.Pid))
	assert.Equal(t, "new", e.executable())
}

func TestMacInstallScript_FailuresKeepTheInstalledBundleAndStartItAgain(t *testing.T) {
	tests := []struct {
		name       string
		env        string
		wantDetach bool // the dmg was mounted, so it is unmounted again
	}{
		{name: "the dmg does not mount", env: "STUB_ATTACH_FAIL=1"},
		{name: "the copy fails", env: "STUB_DITTO_FAIL=1", wantDetach: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newMacScriptEnv(t, "MAM.app")

			require.Error(t, e.run(deadPID(t), tt.env))

			assert.Equal(t, "old", e.executable(), "the installed bundle is untouched")
			assert.Empty(t, e.leftovers())
			calls := strings.Join(e.calls(), "\n")
			assert.Contains(t, calls, "osascript ", "the user is told")
			assert.Contains(t, calls, "open "+e.app, "the installed version is started again")
			assert.Equal(t, tt.wantDetach, strings.Contains(calls, "detach "))
		})
	}
}

// xattr -d fails when there is no quarantine attribute to remove, which is the normal case for a dmg the updater
// downloaded itself: the update goes on.
func TestMacInstallScript_ANoSuchQuarantineAttributeDoesNotStopTheUpdate(t *testing.T) {
	e := newMacScriptEnv(t, "MAM.app")

	require.NoError(t, e.run(deadPID(t), "STUB_XATTR_FAIL=1"))

	assert.Equal(t, "new", e.executable())
	assert.Empty(t, e.leftovers())
	calls := strings.Join(e.calls(), "\n")
	assert.NotContains(t, calls, "osascript ", "no failure dialog")
	assert.Contains(t, calls, "open "+e.app)
}

func TestMacInstallScript_ADmgWithoutTheAppIsRefused(t *testing.T) {
	e := newMacScriptEnv(t, "MAM.app")
	require.NoError(t, os.RemoveAll(filepath.Join(e.fixture, "MAM.app")))
	require.NoError(t, os.MkdirAll(filepath.Join(e.fixture, "Something.app"), 0o750))

	require.Error(t, e.run(deadPID(t)))

	assert.Equal(t, "old", e.executable())
	assert.Empty(t, e.leftovers())
	assert.Contains(t, strings.Join(e.calls(), "\n"), "detach ", "it was mounted, so it is unmounted")
}

// The update script looks for Contents/MacOS/mam in the dmg, and the single instance is keyed on the bundle id that
// cmd/wails uses: the committed Info.plist must agree with both (package-macos.sh builds the binary as MacOS/mam).
func TestInfoPlist_MatchesTheUpdateScriptAndTheSingleInstanceID(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "build", "darwin", "Info.plist"))
	require.NoError(t, err)
	plist := string(data)

	assert.Contains(t, plist, "<key>CFBundleExecutable</key>\n\t<string>mam</string>")
	assert.Contains(t, macInstallScript, `"$src/Contents/MacOS/mam"`)
	assert.Contains(t, plist, "<key>CFBundleIdentifier</key>\n\t<string>com.ousiassllc.mam</string>")
	assert.Contains(t, plist, "@VERSION@", "package-macos.sh fills the release version in")
}
