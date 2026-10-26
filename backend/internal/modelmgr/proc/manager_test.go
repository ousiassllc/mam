package proc_test

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
)

func manager(t *testing.T, mode string) *proc.Manager {
	t.Helper()
	m := proc.New(proc.Config{Build: build(mode), PollEvery: 20 * time.Millisecond, StartTimeout: 20 * time.Second, StopTimeout: 5 * time.Second})
	t.Cleanup(func() { _ = m.Stop() })
	return m
}

type events struct {
	mu sync.Mutex
	st []proc.State
}

func (e *events) add(s proc.Status) { e.mu.Lock(); e.st = append(e.st, s.State); e.mu.Unlock() }
func (e *events) states() []proc.State {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]proc.State(nil), e.st...)
}

func TestStartRunsTheChildOnALoopbackPortAndStopEndsIt(t *testing.T) {
	m := manager(t, "serve")
	var ev events
	defer m.Watch(ev.add)()

	st, err := m.Start(context.Background())
	require.NoError(t, err)
	assert.Equal(t, proc.StateRunning, st.State)
	assert.Positive(t, st.Port)
	assert.Equal(t, "http://127.0.0.1:"+strconv.Itoa(st.Port), st.URL)
	assert.Positive(t, st.PID)
	assert.Equal(t, st, m.Status())

	// Starting again does not start a second child.
	again, err := m.Start(context.Background())
	require.NoError(t, err)
	assert.Equal(t, st.PID, again.PID)

	require.NoError(t, m.Stop())
	assert.Equal(t, proc.StateStopped, m.Status().State)
	assert.False(t, alive(st.PID), "the child is gone")
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(st.Port), 200*time.Millisecond)
	if err == nil {
		_ = conn.Close()
	}
	assert.Error(t, err, "nothing listens any more")
	assert.Equal(t, []proc.State{proc.StateStarting, proc.StateRunning, proc.StateStopped}, ev.states())

	require.NoError(t, m.Stop(), "stopping a stopped child is not an error")
}

func TestTwoCallersStartOneChild(t *testing.T) {
	m := manager(t, "serve")
	var wg sync.WaitGroup
	pids := make([]int, 4)
	for i := range pids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			st, err := m.Start(context.Background())
			assert.NoError(t, err)
			pids[i] = st.PID
		}()
	}
	wg.Wait()
	for _, p := range pids {
		assert.Equal(t, pids[0], p)
	}
}

// The port that was used last is used again when it is still free (the endpoint address stays the same).
func TestPreferredPortIsUsedWhenFree(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	free := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())
	m := proc.New(proc.Config{Build: build("serve"), PreferredPort: func() int { return free }, PollEvery: 20 * time.Millisecond})
	t.Cleanup(func() { _ = m.Stop() })
	st, err := m.Start(context.Background())
	require.NoError(t, err)
	assert.Equal(t, free, st.Port)
	require.NoError(t, m.Stop())

	// Taken: another port is chosen.
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = busy.Close() }()
	taken := busy.Addr().(*net.TCPAddr).Port
	m2 := proc.New(proc.Config{Build: build("serve"), PreferredPort: func() int { return taken }, PollEvery: 20 * time.Millisecond})
	t.Cleanup(func() { _ = m2.Stop() })
	st, err = m2.Start(context.Background())
	require.NoError(t, err)
	assert.NotEqual(t, taken, st.Port)
}

func TestAChildThatExitsAtOnceIsAFailedStartWithItsLog(t *testing.T) {
	m := manager(t, "exit-at-once")
	var ev events
	defer m.Watch(ev.add)()
	st, err := m.Start(context.Background())
	require.Error(t, err)
	assert.Equal(t, proc.StateFailed, st.State)
	assert.Equal(t, 3, st.ExitCode)
	assert.Contains(t, st.Error, "終了しました")
	assert.Contains(t, st.Error, "ImportError: DLL load failed", "the end of the log tells the user why")
	assert.Equal(t, []proc.State{proc.StateStarting, proc.StateFailed}, ev.states())
}

func TestAChildThatNeverAnswersTimesOutAndIsKilled(t *testing.T) {
	m := proc.New(proc.Config{Build: build("never-ready"), PollEvery: 20 * time.Millisecond, StartTimeout: 300 * time.Millisecond, StopTimeout: 3 * time.Second})
	t.Cleanup(func() { _ = m.Stop() })
	st, err := m.Start(context.Background())
	require.Error(t, err)
	assert.Equal(t, proc.StateFailed, st.State)
	assert.Contains(t, st.Error, "応答しませんでした")
	assert.NoError(t, m.Stop())
}

func TestACrashWhileRunningIsReportedAndTheNextStartWorks(t *testing.T) {
	failed := make(chan proc.Status, 1)
	m := manager(t, "crash-later")
	defer m.Watch(func(s proc.Status) {
		if s.State == proc.StateFailed {
			select {
			case failed <- s:
			default:
			}
		}
	})()
	st, err := m.Start(context.Background())
	require.NoError(t, err)
	select {
	case s := <-failed:
		assert.Equal(t, 9, s.ExitCode)
		assert.Contains(t, s.Error, "予期せず終了")
		assert.Contains(t, s.Error, "CUDA out of memory")
		assert.Equal(t, st.Port, s.Port)
	case <-time.After(10 * time.Second):
		t.Fatal("no failure was reported")
	}
	assert.Equal(t, proc.StateFailed, m.Status().State)
	assert.False(t, alive(st.PID))
	_, err = m.Start(context.Background())
	assert.NoError(t, err, "a failed child can be started again")
}

func TestTheOutputOfTheChildIsWrittenToTheLogFile(t *testing.T) {
	log := filepath.Join(t.TempDir(), "logs", "comfyui.log")
	m := proc.New(proc.Config{
		PollEvery: 20 * time.Millisecond,
		Build: func(port int) (proc.Spec, error) {
			s, err := build("serve")(port)
			s.LogPath = log
			return s, err
		},
	})
	t.Cleanup(func() { _ = m.Stop() })
	_, err := m.Start(context.Background())
	require.NoError(t, err)
	require.NoError(t, m.Stop())
	data, err := os.ReadFile(log)
	require.NoError(t, err)
	assert.Contains(t, string(data), "fake comfyui starting")
}

func TestStartRefusesWhenTheCommandCannotBeBuiltOrRun(t *testing.T) {
	m := proc.New(proc.Config{Build: func(int) (proc.Spec, error) { return proc.Spec{}, assert.AnError }})
	st, err := m.Start(context.Background())
	require.ErrorIs(t, err, assert.AnError)
	assert.Equal(t, proc.StateFailed, st.State)

	m = proc.New(proc.Config{Build: func(int) (proc.Spec, error) {
		return proc.Spec{Command: filepath.Join(t.TempDir(), "no-such-python.exe")}, nil
	}})
	st, err = m.Start(context.Background())
	require.Error(t, err)
	assert.Equal(t, proc.StateFailed, st.State)
	assert.Contains(t, st.Error, "起動できません")
}

func TestCallerContextEndsTheWaitForAStartInProgress(t *testing.T) {
	m := proc.New(proc.Config{Build: build("never-ready"), PollEvery: 20 * time.Millisecond, StartTimeout: time.Minute, StopTimeout: 3 * time.Second})
	t.Cleanup(func() { _ = m.Stop() })
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, err := m.Start(ctx)
	require.Error(t, err)
}
