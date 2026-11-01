package proc_test

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
)

// A runtime other than ComfyUI names itself in the messages and says which path answers once it is up.
func TestAnotherProgramNamesItselfAndIsReadyOnItsOwnPath(t *testing.T) {
	buildRuntime := func(port int) (proc.Spec, error) {
		return proc.Spec{Command: os.Args[0], Env: []string{"MAM_FAKE_RUNTIME=1", "FAKE_PORT=" + strconv.Itoa(port)}}, nil
	}
	m := proc.New(proc.Config{Name: "GPT-SoVITS", Build: buildRuntime, Ready: proc.HTTPReady("/openapi.json"), PollEvery: 20 * time.Millisecond, StartTimeout: 20 * time.Second})
	t.Cleanup(func() { _ = m.Stop() })
	st, err := m.Start(context.Background())
	require.NoError(t, err)
	assert.Equal(t, proc.StateRunning, st.State)
	require.NoError(t, m.Stop())
}

func TestTheDefaultReadyPathIsComfyUIsAndAnotherProgramNeverAnswersIt(t *testing.T) {
	buildRuntime := func(port int) (proc.Spec, error) {
		return proc.Spec{Command: os.Args[0], Env: []string{"MAM_FAKE_RUNTIME=1", "FAKE_PORT=" + strconv.Itoa(port)}}, nil
	}
	m := proc.New(proc.Config{Name: "GPT-SoVITS", Build: buildRuntime, PollEvery: 20 * time.Millisecond, StartTimeout: 400 * time.Millisecond})
	t.Cleanup(func() { _ = m.Stop() })
	st, err := m.Start(context.Background())
	require.Error(t, err)
	assert.Equal(t, proc.StateFailed, st.State)
	assert.Contains(t, st.Error, "GPT-SoVITS が起動しませんでした", "the message names the program, not ComfyUI")
	assert.NotContains(t, st.Error, "ComfyUI")
}

func TestAnExitNobodyAskedForNamesTheProgram(t *testing.T) {
	m := proc.New(proc.Config{Name: "GPT-SoVITS", Build: build("exit-at-once"), PollEvery: 20 * time.Millisecond, StartTimeout: 20 * time.Second})
	st, err := m.Start(context.Background())
	require.Error(t, err)
	assert.Contains(t, st.Error, "GPT-SoVITS が起動しませんでした")
	assert.Contains(t, st.Error, "ImportError: DLL load failed", "the end of the log is in the error")
}
