package llamaservertest_test

import (
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
)

// A llama.cpp that was installed again is not the one that failed to start before (#1167): the install takes the failed state, its error and the end of the
// log away, and a start that fails after it shows the end of its own run only, though the log file holds both runs.
func TestInstallingLlamaCppAgainForgetsThePreviousStartFailure(t *testing.T) {
	var run atomic.Int32
	r := newRig(t, func(_ *rig, d *localruntime.Deps) {
		d.Env = func(string) []string {
			if run.Load() == 0 {
				return []string{"LLAMA_FAKE_SAYS=FIRST-RUN error: failed to load model"}
			}
			return []string{"LLAMA_FAKE_SAYS=SECOND-RUN error: CUDA out of memory"}
		}
	})
	r.install()
	_, err := r.svc.Start(ctx(), "llama-cpp")
	require.Error(t, err)
	v, _ := r.svc.Status(ctx(), "llama-cpp")
	require.Equal(t, proc.StateFailed, v.Process.State)
	require.Contains(t, v.Process.Error, "FIRST-RUN")
	require.Contains(t, v.LogTail, "FIRST-RUN")

	ref, err := r.svc.Install(ctx(), "llama-cpp") // the update
	require.NoError(t, err)
	require.Equal(t, domain.JobSucceeded, r.run(ref).State)

	v, _ = r.svc.Status(ctx(), "llama-cpp")
	assert.Equal(t, proc.Status{State: proc.StateStopped}, v.Process, "no failed state, no error")
	assert.Empty(t, v.LogTail, "the log of the files that are gone is not the log of the new ones")

	run.Store(1)
	_, err = r.svc.Start(ctx(), "llama-cpp")
	require.Error(t, err)
	v, _ = r.svc.Status(ctx(), "llama-cpp")
	assert.Contains(t, v.Process.Error, "SECOND-RUN")
	assert.NotContains(t, v.Process.Error, "FIRST-RUN")
	assert.Contains(t, v.LogTail, "SECOND-RUN")
	assert.NotContains(t, v.LogTail, "FIRST-RUN", "the end of the log is of the run that failed now")
}

// Repairing the weights (the install of the weights alone) forgets the failure too: a start that failed on a damaged file is not the failure of the repaired one.
func TestInstallingTheWeightsAgainForgetsThePreviousStartFailure(t *testing.T) {
	r := newRig(t, func(_ *rig, d *localruntime.Deps) {
		d.Env = func(string) []string { return []string{"LLAMA_FAKE_SAYS=FIRST-RUN error: failed to load model"} }
	})
	r.install()
	_, err := r.svc.Start(ctx(), "llama-cpp")
	require.Error(t, err)

	require.Equal(t, domain.JobSucceeded, r.installWeights().State)

	v, _ := r.svc.Status(ctx(), "llama-cpp")
	assert.Equal(t, proc.Status{State: proc.StateStopped}, v.Process)
	assert.Empty(t, v.LogTail)
}
