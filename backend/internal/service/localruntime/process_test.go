package localruntime_test

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/gpu"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	rtm "github.com/ousiassllc/mam/backend/internal/modelmgr/runtime"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
)

func TestStartRunsTheServerAndStopEndsIt(t *testing.T) {
	r := newRig(t)
	_, err := r.svc.Start(ctx(), "gpt-sovits")
	require.ErrorIs(t, err, localruntime.ErrNotInstalled)
	r.install()

	var states []proc.State
	cancel := r.svc.Watch(func(e localruntime.Event) {
		if e.Process != nil && e.Runtime == "gpt-sovits" {
			states = append(states, e.Process.State)
		}
	})
	defer cancel()
	st, err := r.svc.Start(ctx(), "gpt-sovits")
	require.NoError(t, err)
	assert.Equal(t, proc.StateRunning, st.State)
	assert.Contains(t, st.URL, "http://127.0.0.1:", "loopback only")
	assert.Positive(t, st.PID)
	v, _ := r.svc.Status(ctx(), "gpt-sovits")
	assert.Equal(t, proc.StateRunning, v.Process.State)

	again, err := r.svc.Start(ctx(), "gpt-sovits")
	require.NoError(t, err)
	assert.Equal(t, st.PID, again.PID, "starting a running server is a no-op")

	require.NoError(t, r.svc.Stop("gpt-sovits"))
	v, _ = r.svc.Status(ctx(), "gpt-sovits")
	assert.Equal(t, proc.StateStopped, v.Process.State)
	assert.Equal(t, []proc.State{proc.StateStarting, proc.StateRunning, proc.StateStopped}, states)
	require.NoError(t, r.svc.Stop("gpt-sovits"), "stopping a stopped server is a no-op")
	assert.ErrorIs(t, r.svc.Stop("nope"), localruntime.ErrNotFound)
}

func TestTheLastPortIsTriedFirstAndPrepareStartsLazily(t *testing.T) {
	r := newRig(t)
	r.install()
	require.NoError(t, r.svc.Prepare(ctx(), "gpt-sovits"), "the first call of a provider starts the server")
	first, _ := r.svc.Status(ctx(), "gpt-sovits")
	require.Equal(t, proc.StateRunning, first.Process.State)
	require.NoError(t, r.svc.Prepare(ctx(), "gpt-sovits"), "a running server is left alone")
	require.NoError(t, r.svc.Stop("gpt-sovits"))

	_, err := r.svc.Start(ctx(), "gpt-sovits")
	require.NoError(t, err)
	second, _ := r.svc.Status(ctx(), "gpt-sovits")
	assert.Equal(t, first.Process.Port, second.Process.Port, "the address stays the same from run to run")
	assert.ErrorIs(t, r.svc.Prepare(ctx(), "nope"), localruntime.ErrNotFound)
}

func TestAStartThatFailsIsFailedWithTheErrorAndTheEndOfTheLog(t *testing.T) {
	r := newRig(t)
	r.install()
	r.mode = "exit"
	_, err := r.svc.Start(ctx(), "gpt-sovits")
	require.Error(t, err)
	v, _ := r.svc.Status(ctx(), "gpt-sovits")
	assert.Equal(t, proc.StateFailed, v.Process.State)
	assert.Contains(t, v.Process.Error, "GPT-SoVITS が起動しませんでした")
	assert.Contains(t, v.Process.Error, "DLL load failed", "the end of the log is in the error")
	assert.Contains(t, v.LogTail, "DLL load failed", "and in the log tail of the status")
	assert.True(t, v.Installed, "a failed start keeps the install")

	r.mode = "serve"
	st, err := r.svc.Start(ctx(), "gpt-sovits")
	require.NoError(t, err, "the next start tries again")
	assert.Equal(t, proc.StateRunning, st.State)
}

func TestARunningServerIsStoppedBeforeItsFilesAreReplacedAndByRemove(t *testing.T) {
	r := newRig(t)
	r.install()
	_, err := r.svc.Start(ctx(), "gpt-sovits")
	require.NoError(t, err)

	ref, err := r.svc.Install(ctx(), "gpt-sovits") // the update of a running runtime
	require.NoError(t, err)
	require.Equal(t, "succeeded", string(r.wait(ref.JobID).State))
	v, _ := r.svc.Status(ctx(), "gpt-sovits")
	assert.Equal(t, proc.StateStopped, v.Process.State)

	_, err = r.svc.Start(ctx(), "gpt-sovits")
	require.NoError(t, err)
	require.NoError(t, r.svc.Remove(ctx(), "gpt-sovits"))
	v, _ = r.svc.Status(ctx(), "gpt-sovits")
	assert.Equal(t, proc.StateStopped, v.Process.State)
	assert.False(t, v.Installed)
	assert.ErrorIs(t, r.svc.Remove(ctx(), "nope"), localruntime.ErrNotFound)
}

func TestAutoStartAtStartupAndCleanupOfWhatAnInterruptedInstallLeft(t *testing.T) {
	r := newRig(t)
	r.install()
	require.NoError(t, r.svc.SetAutoStart("gpt-sovits", true))
	require.NoError(t, os.MkdirAll(r.layout.EnvNewDir(), 0o750))
	v, _ := r.svc.Status(ctx(), "gpt-sovits")
	assert.True(t, v.AutoStart)

	r.svc.OnStartup(context.Background())
	assert.NoDirExists(t, r.layout.EnvNewDir(), "the half-extracted environment is removed")
	require.Eventually(t, func() bool {
		v, _ := r.svc.Status(ctx(), "gpt-sovits")
		return v.Process.State == proc.StateRunning
	}, 20*time.Second, 20*time.Millisecond, "a runtime that is set to start with mam starts, without blocking OnStartup")

	r.svc.Close()
	v, _ = r.svc.Status(ctx(), "gpt-sovits")
	assert.Equal(t, proc.StateStopped, v.Process.State, "Close stops the server")
	assert.ErrorIs(t, r.svc.SetAutoStart("nope", true), localruntime.ErrNotFound)
}

// A log that a test can read while the server's goroutine writes to it.
type syncBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestNothingStartsWithoutTheSettingAndAnAutoStartOnAMachineThatLostItsGPUIsRefusedAndLogged(t *testing.T) {
	logs := &syncBuffer{}
	r := newRig(t, func(_ *rig, d *localruntime.Deps) { d.Logger = slog.New(slog.NewTextHandler(logs, nil)) })
	r.install()
	r.svc.OnStartup(context.Background()) // the default: lazy, so no start is made at all
	v, _ := r.svc.Status(ctx(), "gpt-sovits")
	assert.Equal(t, proc.StateStopped, v.Process.State)

	require.NoError(t, r.svc.SetAutoStart("gpt-sovits", true))
	r.gpu = gpu.Report{} // the GPU is gone
	r.svc.OnStartup(context.Background())
	require.Eventually(t, func() bool { return strings.Contains(logs.String(), "start the local runtime failed") }, 20*time.Second, 10*time.Millisecond)
	v, _ = r.svc.Status(ctx(), "gpt-sovits")
	assert.Equal(t, proc.StateStopped, v.Process.State, "the refused start never launched a process")
	_, err := r.svc.Start(ctx(), "gpt-sovits")
	var se *localruntime.SupportError
	require.ErrorAs(t, err, &se)
	assert.Equal(t, rtm.CodeNoGPU, se.Support.Code)
}
