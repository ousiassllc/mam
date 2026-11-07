package runtimetest_test

import (
	"os"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/service/llamaserver"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmview"
)

// heldBy makes the runtime "pulling": while the returned value holds a model name, HeldBy names it for every runtime (the app answers for the Ollama only).
func heldBy() (*atomic.Value, option) {
	var pulling atomic.Value
	pulling.Store("")
	return &pulling, func(_ *rig, d *localruntime.Deps) { d.HeldBy = func(string) string { return pulling.Load().(string) } }
}

// While a model is being fetched into the runtime (the Ollama's pull), Remove, Install, InstallWeights and Stop are refused with ErrBusy that names the model: the server
// keeps running, no file is deleted and no install job is queued; once the fetch ended they go through.
func TestRemoveInstallAndStopAreBusyWhileAModelIsBeingFetched(t *testing.T) {
	pulling, opt := heldBy()
	r := newRig(t, opt)
	r.install()
	_, err := r.svc.Start(ctx(), "gpt-sovits")
	require.NoError(t, err)
	pulling.Store("gemma4:12b")

	for name, err := range map[string]error{
		"Stop":           r.svc.Stop("gpt-sovits"),
		"Remove":         r.svc.Remove(ctx(), "gpt-sovits"),
		"Install":        func() error { _, err := r.svc.Install(ctx(), "gpt-sovits"); return err }(),
		"InstallWeights": func() error { _, err := r.svc.InstallWeights(ctx(), "gpt-sovits"); return err }(),
	} {
		require.ErrorIs(t, err, localruntime.ErrBusy, name)
		assert.ErrorContains(t, err, "gemma4:12b", name)
		kind, msg := mmview.RefuseRuntime(err)
		assert.Equal(t, mmview.RefusalConflict, kind, name)
		assert.Contains(t, msg, "gemma4:12b", name)
		assert.Contains(t, llamaserver.Explain(err), "gemma4:12b", name)
	}
	v, err := r.svc.Status(ctx(), "gpt-sovits")
	require.NoError(t, err)
	assert.Equal(t, proc.StateRunning, v.Process.State, "the server was not stopped")
	assert.True(t, v.Installed)
	assert.Empty(t, v.InstallJob, "no install was queued")
	_, err = os.Stat(r.layout.EnvDir())
	assert.NoError(t, err, "the files were not removed")

	pulling.Store("") // the fetch ended
	require.NoError(t, r.svc.Stop("gpt-sovits"))
	ref, err := r.svc.Install(ctx(), "gpt-sovits")
	require.NoError(t, err)
	require.Equal(t, "succeeded", string(r.wait(ref.JobID).State))
	require.NoError(t, r.svc.Remove(ctx(), "gpt-sovits"))
	v, _ = r.svc.Status(ctx(), "gpt-sovits")
	assert.False(t, v.Installed)
}
