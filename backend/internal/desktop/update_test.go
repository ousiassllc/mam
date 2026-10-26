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
	u.Install("/tmp/setup.exe")

	require.ErrorIs(t, u.RunPending(), boom)
}
