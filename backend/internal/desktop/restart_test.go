package desktop

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelaunchCommand_WindowsStartsTheExecutableWithTheOldPidToWaitFor(t *testing.T) {
	cmd := RelaunchCommand("windows", 4242, `C:\mam\mam.exe`, []string{"--data-dir", "D:\\x"}, []string{"A=1", restartWaitEnv + "=7", "B=2"})

	assert.Equal(t, `C:\mam\mam.exe`, cmd.Path)
	assert.Equal(t, []string{`C:\mam\mam.exe`, "--data-dir", "D:\\x"}, cmd.Args)
	assert.Equal(t, []string{"A=1", "B=2", restartWaitEnv + "=4242"}, cmd.Env, "an old value is replaced, the rest of the environment is kept")
	assert.NotNil(t, cmd.SysProcAttr, "detached from this process")
}

func TestRelaunchCommand_MacOpensTheBundleAfterTheHelperWaitedForThePid(t *testing.T) {
	cmd := RelaunchCommand("darwin", 4242, "/Applications/MAM.app/Contents/MacOS/mam", []string{"--port", "9000"}, nil)

	assert.Equal(t, "/bin/sh", cmd.Args[0])
	assert.Equal(t, []string{"-c", macRestartScript, "mam-restart", "4242", "/usr/bin/open", "/Applications/MAM.app", "--args", "--port", "9000"}, cmd.Args[1:])
	assert.Contains(t, macRestartScript, `kill -0 "$pid"`)
}

func TestRelaunchCommand_MacOutsideABundleRunsTheExecutableItselfAndWithoutArgsHasNoArgsMarker(t *testing.T) {
	cmd := RelaunchCommand("darwin", 7, "/tmp/mam", nil, nil)
	assert.Equal(t, []string{"-c", macRestartScript, "mam-restart", "7", "/tmp/mam"}, cmd.Args[1:])

	cmd = RelaunchCommand("darwin", 7, "/Applications/MAM.app/Contents/MacOS/mam", nil, nil)
	assert.Equal(t, []string{"-c", macRestartScript, "mam-restart", "7", "/usr/bin/open", "/Applications/MAM.app"}, cmd.Args[1:])
}

func TestRestarter_QuitsOnRequestAndStartsTheApplicationOnlyAfterwards(t *testing.T) {
	quit := make(chan struct{}, 1)
	var started atomic.Pointer[exec.Cmd]
	r := NewRestarter(func() { quit <- struct{}{} })
	want := exec.Command("mam-new")
	r.command = func() (*exec.Cmd, error) { return want, nil }
	r.start = func(c *exec.Cmd) error { started.Store(c); return nil }

	require.NoError(t, r.RunPending())
	assert.Nil(t, started.Load(), "nothing starts for a shutdown nobody asked a restart for")
	assert.False(t, r.Requested())

	r.Restart()
	<-quit
	assert.True(t, r.Requested())
	assert.Nil(t, started.Load(), "the application is started by RunPending, once the server stopped, not by Restart")

	require.NoError(t, r.RunPending())
	assert.Same(t, want, started.Load())
}

func TestRestarter_ReportsAStartThatFailed(t *testing.T) {
	r := NewRestarter(func() {})
	boom := errors.New("cannot start")
	r.command = func() (*exec.Cmd, error) { return exec.Command("x"), nil }
	r.start = func(*exec.Cmd) error { return boom }
	r.requested = true

	assert.ErrorIs(t, r.RunPending(), boom)
}

func TestUpdater_PendingIsTrueOnceAnInstallerWasHandedOver(t *testing.T) {
	u := NewUpdater(func() {})
	assert.False(t, u.Pending())
	u.Install("setup.exe")
	assert.True(t, u.Pending())
}

func TestWaitForPredecessor_WaitsForTheOldProcessAndClearsTheVariable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a Unix helper process")
	}
	sleeper := exec.Command("sleep", "60")
	require.NoError(t, sleeper.Start())
	done := make(chan struct{})
	go func() { _ = sleeper.Wait(); close(done) }()
	t.Cleanup(func() { _ = sleeper.Process.Kill(); <-done })

	assert.False(t, waitForExit(sleeper.Process.Pid, 50*time.Millisecond), "a process that is alive is not waited out")

	require.NoError(t, sleeper.Process.Kill())
	<-done // reaped: the pid is gone
	assert.True(t, waitForExit(sleeper.Process.Pid, 5*time.Second))

	t.Setenv(restartWaitEnv, strconv.Itoa(sleeper.Process.Pid))
	WaitForPredecessor()
	_, still := os.LookupEnv(restartWaitEnv)
	assert.False(t, still, "nothing started later inherits the variable")
}

func TestWaitForPredecessor_DoesNothingForAnOrdinaryStart(t *testing.T) {
	t.Setenv(restartWaitEnv, "")
	WaitForPredecessor()
}
