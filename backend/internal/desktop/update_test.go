package desktop

import (
	"errors"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The installer is run as `<installer> /S`: silent, with nothing else (no shell in between).
func TestInstallCommand_RunsTheInstallerSilently(t *testing.T) {
	const installer = `C:\Users\u\AppData\Local\Temp\mam-update-123\mam-0.2.0-windows-amd64-setup.exe`
	cmd := InstallCommand(installer)

	assert.Equal(t, installer, cmd.Path)
	assert.Equal(t, []string{installer, "/S"}, cmd.Args)
	assert.Nil(t, cmd.Cancel, "no context: nothing may kill the installer when this process ends")
}

func TestUpdater_InstallQuitsWithoutBlockingAndRunPendingStartsTheInstaller(t *testing.T) {
	quit := make(chan struct{})
	release := make(chan struct{}) // the quit does not return until the test lets it
	u := NewUpdater(func() { close(quit); <-release })
	var started []*exec.Cmd
	u.start = func(c *exec.Cmd) error { started = append(started, c); return nil }
	u.command = func(p string) (*exec.Cmd, error) { return InstallCommand(p), nil } // the Windows command, on any host OS

	done := make(chan struct{})
	go func() { u.Install("/tmp/setup.exe"); close(done) }()
	select {
	case <-done: // Install returned although quit has not
	case <-time.After(5 * time.Second):
		t.Fatal("Install blocked on quit")
	}
	select {
	case <-quit:
	case <-time.After(5 * time.Second):
		t.Fatal("the application was not asked to quit")
	}
	close(release)

	require.NoError(t, u.RunPending())
	require.Len(t, started, 1)
	assert.Equal(t, []string{"/tmp/setup.exe", "/S"}, started[0].Args)
}

func TestUpdater_RunPendingWithoutAnUpdateDoesNothing(t *testing.T) {
	u := NewUpdater(func() {})
	u.start = func(*exec.Cmd) error { t.Fatal("an installer was started without an update"); return nil }

	require.NoError(t, u.RunPending())
}

func TestUpdater_RunPendingReturnsTheFailureToStart(t *testing.T) {
	u := NewUpdater(func() {})
	boom := errors.New("access denied")
	u.start = func(*exec.Cmd) error { return boom }
	u.command = func(p string) (*exec.Cmd, error) { return InstallCommand(p), nil }
	u.Install("/tmp/setup.exe")

	require.ErrorIs(t, u.RunPending(), boom)
}

// The automatic update is on only where a release carries an installer: on every other GOOS/GOARCH the front
// gets an error and passes no Update option, so the Checker (and its choice of an installer) is never built.
func TestCheckCanInstallOn_OnlyWherePlatformHasAnInstaller(t *testing.T) {
	macBundle := func() (string, error) { return "/Applications/MAM.app", nil }
	noBundle := func() (string, error) {
		t.Fatal("the bundle of an unsupported platform was looked for")
		return "", nil
	}

	require.NoError(t, checkCanInstallOn("windows", "amd64", noBundle), "Windows installs through its own installer")
	require.NoError(t, checkCanInstallOn("darwin", "arm64", macBundle))
	for _, p := range [][2]string{{"darwin", "amd64"}, {"windows", "arm64"}, {"linux", "amd64"}, {"", ""}} {
		err := checkCanInstallOn(p[0], p[1], noBundle)
		require.Error(t, err, "%s/%s", p[0], p[1])
		assert.Contains(t, err.Error(), "no installer is released")
	}
}

func TestCheckCanInstallOn_MacNeedsAReplaceableBundle(t *testing.T) {
	boom := errors.New("running from a disk image")
	err := checkCanInstallOn("darwin", "arm64", func() (string, error) { return "", boom })
	require.ErrorIs(t, err, boom)
}
