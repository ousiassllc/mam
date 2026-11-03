//go:build !windows

package proc

import (
	"context"
	"encoding/binary"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeProcs is the table of processes of the OS: probe reads it, signal changes it.
type fakeProcs struct {
	mu        sync.Mutex
	ids       map[int]processID
	endOnTerm bool // false: the process ignores the interrupt
	sigs      []bool
}

func (f *fakeProcs) probe(pid int) (processID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, ok := f.ids[pid]
	if !ok {
		return processID{}, errNoProcess
	}
	return id, nil
}

func (f *fakeProcs) signal(pid int, force bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sigs = append(f.sigs, force)
	if force || f.endOnTerm {
		delete(f.ids, pid)
	}
}

func (f *fakeProcs) signals() []bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]bool(nil), f.sigs...)
}

var (
	engine = processID{PID: 4242, Start: "100", Cmdline: "python main.py --port 8188"}
	oldMam = processID{PID: 3000, Start: "50", Cmdline: "mam"}
)

// leftover writes the pid file that a mam which was killed left, and returns its path and the OS around it.
func leftover(t *testing.T, procs map[int]processID) (string, *fakeProcs) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "comfyui.pid")
	require.NoError(t, writeJSON(path, pidRecord{Child: engine, Owner: oldMam}))
	return path, &fakeProcs{ids: procs, endOnTerm: true}
}

func reclaimWith(path string, f *fakeProcs) {
	reclaim(path, f.probe, f.signal, 50*time.Millisecond, time.Millisecond, slog.New(slog.DiscardHandler))
}

func TestTheNextStartEndsTheEngineThatTheKilledMamLeft(t *testing.T) {
	path, f := leftover(t, map[int]processID{engine.PID: engine}) // the old mam (3000) is gone
	reclaimWith(path, f)
	assert.Equal(t, []bool{false}, f.signals(), "the group is asked to end")
	assert.NoFileExists(t, path)
	_, err := f.probe(engine.PID)
	assert.ErrorIs(t, err, errNoProcess)
}

func TestAnEngineThatIgnoresTheInterruptIsKilled(t *testing.T) {
	path, f := leftover(t, map[int]processID{engine.PID: engine})
	f.endOnTerm = false
	reclaimWith(path, f)
	assert.Equal(t, []bool{false, true}, f.signals())
	assert.NoFileExists(t, path)
}

// The pid of the file may belong to something else by now: only the process that has the written start time and command line is ended.
func TestAReusedPidIsNeverEnded(t *testing.T) {
	for name, other := range map[string]processID{
		"another start time":   {PID: engine.PID, Start: "999", Cmdline: engine.Cmdline},
		"another command line": {PID: engine.PID, Start: engine.Start, Cmdline: "vim notes.txt"},
	} {
		t.Run(name, func(t *testing.T) {
			path, f := leftover(t, map[int]processID{engine.PID: other})
			reclaimWith(path, f)
			assert.Empty(t, f.signals())
			assert.NoFileExists(t, path, "the stale file is removed")
			_, err := f.probe(engine.PID)
			assert.NoError(t, err, "the other process lives")
		})
	}
}

// Two mam of one directory: the one that wrote the file still runs and owns its engine.
func TestAnEngineOfAMamThatStillRunsIsLeftAlone(t *testing.T) {
	path, f := leftover(t, map[int]processID{engine.PID: engine, oldMam.PID: oldMam})
	reclaimWith(path, f)
	assert.Empty(t, f.signals())
	assert.FileExists(t, path)
}

func TestAnOwnerPidThatIsReusedDoesNotProtectTheEngine(t *testing.T) {
	path, f := leftover(t, map[int]processID{engine.PID: engine, oldMam.PID: {PID: oldMam.PID, Start: "777", Cmdline: "firefox"}})
	reclaimWith(path, f)
	assert.Equal(t, []bool{false}, f.signals())
}

func TestNoFileOrABrokenFileEndsNothing(t *testing.T) {
	f := &fakeProcs{ids: map[int]processID{engine.PID: engine}}
	path := filepath.Join(t.TempDir(), "comfyui.pid")
	reclaimWith(path, f)
	assert.Empty(t, f.signals())

	require.NoError(t, os.WriteFile(path, []byte("{not json"), 0o600))
	reclaimWith(path, f)
	assert.Empty(t, f.signals())
	assert.NoFileExists(t, path)
}

func TestTheEnginePidAndTheOwnerAreWrittenAndOnlyTheOwnersFileIsRemoved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "comfyui.pid")
	f := &fakeProcs{ids: map[int]processID{engine.PID: engine, os.Getpid(): {PID: os.Getpid(), Start: "1", Cmdline: "mam"}}}
	require.True(t, writeRecord(path, engine.PID, f.probe, slog.New(slog.DiscardHandler)))
	rec, err := readRecord(path)
	require.NoError(t, err)
	assert.Equal(t, engine, rec.Child)
	assert.Equal(t, os.Getpid(), rec.Owner.PID)

	removeRecord(path, 1) // another run's pid
	assert.FileExists(t, path)
	removeRecord(path, engine.PID)
	assert.NoFileExists(t, path)

	assert.False(t, writeRecord(path, 99999, f.probe, slog.New(slog.DiscardHandler)), "a child that cannot be identified is not noted")
	assert.NoFileExists(t, path)
}

func TestProcArgs2GivesTheArguments(t *testing.T) {
	buf := binary.NativeEndian.AppendUint32(nil, 2)
	buf = append(buf, "/usr/bin/python3\x00\x00\x00python3\x00main.py\x00PATH=/bin\x00"...)
	argv, err := parseProcArgs2(buf)
	require.NoError(t, err)
	assert.Equal(t, []string{"python3", "main.py"}, argv)

	_, err = parseProcArgs2([]byte{1})
	require.Error(t, err)
	_, err = parseProcArgs2(append(buf[:4:4], "/bin/x\x00\x00a\x00"...)) // argc 2, one argument
	require.Error(t, err)
}

// The real OS: a leftover engine (a real process, not a child of this test) is found by its pid file and ended before the next Start builds
// its command (the preferred port is free again); a pid file of an engine that is not there is only removed.
func TestStartEndsARealLeftoverEngineBeforeItPicksThePort(t *testing.T) {
	if _, err := os.Stat("/proc/self/stat"); err != nil {
		t.Skip("needs /proc (Linux)")
	}
	port, err := FreePort(0)
	require.NoError(t, err)
	old := spawnEngine(t, port)
	id, err := probeProcess(old.Process.Pid)
	require.NoError(t, err)
	assert.Contains(t, id.Cmdline, os.Args[0], "the command line is the test binary's")

	// The owner of the file is a mam that has ended: a process that was alive when the file was written.
	dead := spawn(t)
	owner, err := probeProcess(dead.Process.Pid)
	require.NoError(t, err)
	require.NoError(t, dead.Process.Kill())
	require.Eventually(t, func() bool { return gone(dead.Process.Pid) }, 5*time.Second, 5*time.Millisecond)

	path := filepath.Join(t.TempDir(), "comfyui.pid")
	require.NoError(t, writeJSON(path, pidRecord{Child: id, Owner: owner}))
	var goneAtBuild atomic.Bool
	m := New(Config{Build: func(p int) (Spec, error) {
		goneAtBuild.Store(gone(old.Process.Pid))
		return Spec{Command: os.Args[0], Env: []string{"MAM_FAKE_COMFY=serve", "FAKE_PORT=" + strconv.Itoa(p)}}, nil
	}, PreferredPort: func() int { return port }, PIDFile: path, PollEvery: 20 * time.Millisecond, StopTimeout: 5 * time.Second})
	t.Cleanup(func() { _ = m.Stop() })

	st, err := m.Start(context.Background())
	require.NoError(t, err)
	assert.True(t, goneAtBuild.Load(), "the leftover engine was ended before the new one was built (so its port is free again)")
	rec, err := readRecord(path)
	require.NoError(t, err)
	assert.Equal(t, st.PID, rec.Child.PID, "the pid file names the new engine")
	assert.Equal(t, os.Getpid(), rec.Owner.PID)

	require.NoError(t, m.Stop())
	assert.NoFileExists(t, path, "a stopped engine leaves no pid file")
}

// spawnEngine starts the fake engine (it serves on port) in its own group, like a child of a mam that was killed, and waits until it answers.
func spawnEngine(t *testing.T, port int) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "MAM_FAKE_COMFY=serve", "FAKE_PORT="+strconv.Itoa(port))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	require.NoError(t, cmd.Start())
	go func() { _ = cmd.Wait() }()
	t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) })
	require.Eventually(t, func() bool {
		c, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 100*time.Millisecond)
		if err == nil {
			_ = c.Close()
		}
		return err == nil
	}, 10*time.Second, 10*time.Millisecond, "the engine listens")
	return cmd
}
