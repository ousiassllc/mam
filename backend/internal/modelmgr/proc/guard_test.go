//go:build !windows

package proc

import (
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// spawn starts the test binary as an idle child in its own process group (TestMain: MAM_FAKE_COMFY=never-ready sleeps), and reaps it
// when it ends.
func spawn(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "MAM_FAKE_COMFY=never-ready")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	require.NoError(t, cmd.Start())
	go func() { _ = cmd.Wait() }()
	t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) })
	return cmd
}

func gone(pid int) bool {
	_, err := probeProcess(pid)
	return errors.Is(err, errNoProcess)
}

// pollExit is an exitWatch that asks the OS every few milliseconds (the real one on macOS is kqueue).
func pollExit(pid int) (<-chan struct{}, error) {
	done := make(chan struct{})
	go func() {
		for !gone(pid) {
			time.Sleep(10 * time.Millisecond)
		}
		close(done)
	}()
	return done, nil
}

// A mam that is killed (SIGKILL: no Close) must not leave its child: the guard sees the parent end and ends the child's group.
func TestGuardEndsTheChildWhenTheParentIsKilled(t *testing.T) {
	if _, err := os.Stat("/proc/self/stat"); err != nil {
		t.Skip("needs /proc (Linux)")
	}
	parent, child := spawn(t), spawn(t)
	require.False(t, gone(child.Process.Pid))
	guarded := make(chan error, 1)
	go func() {
		guarded <- guard(parent.Process.Pid, child.Process.Pid, pollExit, signalLeftover, 5*time.Second)
	}()

	select {
	case <-guarded:
		t.Fatal("the guard returned while the parent runs")
	case <-time.After(100 * time.Millisecond):
	}
	require.False(t, gone(child.Process.Pid), "the child lives while its parent does")

	require.NoError(t, parent.Process.Kill())
	assert.Eventually(t, func() bool { return gone(child.Process.Pid) }, 10*time.Second, 20*time.Millisecond, "the child ended with its parent")
	require.NoError(t, <-guarded)
}

type recorder struct {
	mu   sync.Mutex
	sigs []bool // force of each signal
}

func (r *recorder) signal(_ int, force bool) {
	r.mu.Lock()
	r.sigs = append(r.sigs, force)
	r.mu.Unlock()
}

func (r *recorder) got() []bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]bool(nil), r.sigs...)
}

// watcher is an exitWatch of channels the test closes.
func watcher(chans map[int]chan struct{}) exitWatch {
	return func(pid int) (<-chan struct{}, error) { return chans[pid], nil }
}

func TestGuardKillsAChildThatIgnoresTheInterruptAfterTheGrace(t *testing.T) {
	parent, child := make(chan struct{}), make(chan struct{})
	var rec recorder
	done := make(chan error, 1)
	go func() {
		done <- guard(10, 20, watcher(map[int]chan struct{}{10: parent, 20: child}), rec.signal, 30*time.Millisecond)
	}()
	close(parent)
	require.NoError(t, <-done)
	assert.Equal(t, []bool{false, true}, rec.got(), "interrupt, then kill")
}

func TestGuardInterruptsOnlyWhenTheChildEndsInTime(t *testing.T) {
	parent, child := make(chan struct{}), make(chan struct{})
	var rec recorder
	done := make(chan error, 1)
	go func() {
		done <- guard(10, 20, watcher(map[int]chan struct{}{10: parent, 20: child}), rec.signal, time.Minute)
	}()
	close(parent)
	assert.Eventually(t, func() bool { return len(rec.got()) == 1 }, 5*time.Second, 5*time.Millisecond)
	close(child)
	require.NoError(t, <-done)
	assert.Equal(t, []bool{false}, rec.got())
}

// The child that has ended is never signalled: its pid (the id of its group) may be somebody else's by then.
func TestGuardLeavesAChildThatEndedBeforeTheParent(t *testing.T) {
	parent, child := make(chan struct{}), make(chan struct{})
	var rec recorder
	close(child)
	close(parent)
	require.NoError(t, guard(10, 20, watcher(map[int]chan struct{}{10: parent, 20: child}), rec.signal, time.Second))
	assert.Empty(t, rec.got())
}

func TestGuardReportsAWatchThatCannotBeSet(t *testing.T) {
	boom := errors.New("kqueue failed")
	err := guard(10, 20, func(int) (<-chan struct{}, error) { return nil, boom }, (&recorder{}).signal, time.Second)
	require.ErrorIs(t, err, boom)
}

// Only a start with exactly the guard's arguments is a guard: a normal mam (or a mam that is given other arguments) goes on.
func TestOnlyTheGuardArgumentsMakeAGuard(t *testing.T) {
	saved := os.Args
	t.Cleanup(func() { os.Args = saved })
	for name, args := range map[string][]string{
		"no arguments":   {"mam"},
		"other argument": {"mam", "--port", "8080"},
		"missing child":  {"mam", guardFlag, "123"},
		"extra argument": {"mam", guardFlag, "123", "456", "789"},
	} {
		os.Args = args
		assert.False(t, RunGuardIfRequested(), name)
	}
}
