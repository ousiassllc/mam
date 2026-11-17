// Package statustest checks the Status a proc.Manager reports for a child that is not ComfyUI (its name, its ready path) and how a failure is
// forgotten (#1167). It is a package of its own because the directory of proc is at its line budget.
package statustest_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
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

// TestMain turns the test binary into the child the tests start: a server of another program than ComfyUI that answers /openapi.json only
// (MAM_FAKE_RUNTIME), or one that writes MAM_FAKE_SAYS to its standard error and ends with code 2 (MAM_FAKE_SAYS).
func TestMain(m *testing.M) {
	if says := os.Getenv("MAM_FAKE_SAYS"); says != "" {
		fmt.Fprintln(os.Stderr, says)
		os.Exit(2)
	}
	if os.Getenv("MAM_FAKE_RUNTIME") != "" {
		l, err := net.Listen("tcp", "127.0.0.1:"+os.Getenv("FAKE_PORT"))
		if err != nil {
			os.Exit(4)
		}
		mux := http.NewServeMux()
		mux.HandleFunc("/openapi.json", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) })
		_ = http.Serve(l, mux)
		return
	}
	os.Exit(m.Run())
}

func buildRuntime(port int) (proc.Spec, error) {
	return proc.Spec{Command: os.Args[0], Env: []string{"MAM_FAKE_RUNTIME=1", "FAKE_PORT=" + strconv.Itoa(port)}}, nil
}

// A runtime other than ComfyUI names itself in the messages and says which path answers once it is up.
func TestAnotherProgramNamesItselfAndIsReadyOnItsOwnPath(t *testing.T) {
	m := proc.New(proc.Config{Name: "GPT-SoVITS", Build: buildRuntime, Ready: proc.HTTPReady("/openapi.json"), PollEvery: 20 * time.Millisecond, StartTimeout: 20 * time.Second})
	t.Cleanup(func() { _ = m.Stop() })
	st, err := m.Start(context.Background())
	require.NoError(t, err)
	assert.Equal(t, proc.StateRunning, st.State)
	require.NoError(t, m.Stop())
}

func TestTheDefaultReadyPathIsComfyUIsAndAnotherProgramNeverAnswersIt(t *testing.T) {
	m := proc.New(proc.Config{Name: "GPT-SoVITS", Build: buildRuntime, PollEvery: 20 * time.Millisecond, StartTimeout: 400 * time.Millisecond})
	t.Cleanup(func() { _ = m.Stop() })
	st, err := m.Start(context.Background())
	require.Error(t, err)
	assert.Equal(t, proc.StateFailed, st.State)
	assert.Contains(t, st.Error, "GPT-SoVITS が起動しませんでした", "the message names the program, not ComfyUI")
	assert.NotContains(t, st.Error, "ComfyUI")
}

// A failed start is forgotten by ClearFailure (the engine was installed again), and the failure of the next start shows the end of its own run
// only, though both runs wrote to the same log file.
func TestClearFailureForgetsTheFailureAndTheNextOneShowsOnlyItsOwnLog(t *testing.T) {
	log := filepath.Join(t.TempDir(), "logs", "comfyui.log")
	says := []string{"first run: main.py: No such file", "second run: DLL load failed"}
	runs := 0
	m := proc.New(proc.Config{
		PollEvery: 20 * time.Millisecond,
		Build: func(int) (proc.Spec, error) {
			runs++
			return proc.Spec{Command: os.Args[0], Env: []string{"MAM_FAKE_SAYS=" + says[runs-1]}, LogPath: log}, nil
		},
	})
	var mu sync.Mutex
	var states []proc.State
	defer m.Watch(func(s proc.Status) { mu.Lock(); states = append(states, s.State); mu.Unlock() })()

	st, err := m.Start(context.Background())
	require.Error(t, err)
	require.Equal(t, proc.StateFailed, st.State)
	assert.Contains(t, st.Error, says[0])

	m.ClearFailure()
	assert.Equal(t, proc.Status{State: proc.StateStopped}, m.Status(), "no badge, no error, no end of the log")
	mu.Lock()
	assert.Equal(t, []proc.State{proc.StateStarting, proc.StateFailed, proc.StateStopped}, states, "the screen is told")
	mu.Unlock()

	st, err = m.Start(context.Background())
	require.Error(t, err)
	require.Equal(t, proc.StateFailed, st.State)
	assert.Contains(t, st.Error, says[1])
	assert.NotContains(t, st.Error, says[0], "the end of the log is of this run, not of the one before")
	data, err := os.ReadFile(log)
	require.NoError(t, err)
	assert.Contains(t, string(data), says[0], "the file keeps both runs; the failure must not be read from it")
}

// Only a failure is forgotten: a running child stays running.
func TestClearFailureLeavesARunningChildAlone(t *testing.T) {
	m := proc.New(proc.Config{Name: "GPT-SoVITS", Build: buildRuntime, Ready: proc.HTTPReady("/openapi.json"), PollEvery: 20 * time.Millisecond, StartTimeout: 20 * time.Second})
	t.Cleanup(func() { _ = m.Stop() })
	st, err := m.Start(context.Background())
	require.NoError(t, err)
	m.ClearFailure()
	assert.Equal(t, st, m.Status())
}
