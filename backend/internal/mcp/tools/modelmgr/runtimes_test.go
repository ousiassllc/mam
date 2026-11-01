package modelmgr_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/modelmgr"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime/lrtest"
)

func installRuntime(t *testing.T, env *mcptest.Env, mm interface{ Start() }) {
	t.Helper()
	var out modelmgr.JobOutput
	env.CallOK("install_local_runtime", map[string]any{"runtime_id": lrtest.ID}, &out)
	assert.Equal(t, "runtime:gpt-sovits", out.Target)
	mm.Start()
	st := pollJob(t, env, out.JobID)
	require.Equal(t, mcpkit.JobSucceeded, st.State, st.Error)
}

func pollJob(t *testing.T, env *mcptest.Env, id string) mcpkit.JobStatus {
	t.Helper()
	var st mcpkit.JobStatus
	require.Eventually(t, func() bool {
		env.CallOK("get_job", map[string]any{"job_id": id}, &st)
		return st.State.Final()
	}, 30e9, 20e6)
	return st
}

func TestRuntimes_ListShowsStateSupportAndTheWeightsWithSourceAndLicense(t *testing.T) {
	env, _, _ := newRuntimeEnv(t)
	var list modelmgr.ListRuntimesOutput
	env.CallOK("list_local_runtimes", map[string]any{}, &list)
	require.Len(t, list.Runtimes, 1)
	v := list.Runtimes[0]
	assert.Equal(t, "gpt-sovits", v.ID)
	assert.True(t, v.Support.Supported)
	assert.False(t, v.Installed)
	assert.Equal(t, localruntime.WeightsNone, v.Weights.State)
	require.Len(t, v.Weights.Assets, 2)
	assert.Equal(t, 1, v.Weights.Unclear)
	w := v.Weights.Assets[0]
	assert.Equal(t, "https://huggingface.co/org/repo", w.Source.URL)
	assert.Equal(t, catalog.CommercialYes, w.License.CommercialUse)
	x := v.Weights.Assets[1]
	assert.Equal(t, catalog.CommercialUnclear, x.License.CommercialUse)
	assert.NotEmpty(t, x.License.Notice, "an unclear license carries what to watch for")
	assert.Contains(t, x.Source.Note, "2026-11-01")

	var one localruntime.View
	env.CallOK("get_local_runtime_status", map[string]any{"runtime_id": "gpt-sovits"}, &one)
	assert.Equal(t, v.ID, one.ID)
	assert.Contains(t, env.CallError("get_local_runtime_status", map[string]any{"runtime_id": "nope"}), "not_found")
}

func TestRuntimes_ACloudFallbackIsInTheRefusalOfAMachineThatCannotRunIt(t *testing.T) {
	env, _, _ := newRuntimeEnv(t, lrtest.Platform("darwin", "arm64"))
	var list modelmgr.ListRuntimesOutput
	env.CallOK("list_local_runtimes", map[string]any{}, &list)
	v := list.Runtimes[0]
	assert.False(t, v.Support.Supported)
	assert.Contains(t, v.Support.Reason, "macOS では GPT-SoVITS の自動導入に対応していません")
	assert.Contains(t, v.Support.Fallback, "ElevenLabs")
	assert.Len(t, v.Weights.Assets, 2, "the sources and licenses are readable here too")

	for _, tool := range []string{"install_local_runtime", "start_local_runtime"} {
		msg := env.CallError(tool, map[string]any{"runtime_id": "gpt-sovits"})
		assert.Contains(t, msg, "conflict", tool)
		assert.Contains(t, msg, "macOS では GPT-SoVITS の自動導入に対応していません", tool)
		assert.Contains(t, msg, "ElevenLabs", tool)
	}
}

func TestRuntimes_InstallIsAJobAndAMissingWeightStopsTheStartUntilInstallWeightsFixesIt(t *testing.T) {
	env, mm, lr := newRuntimeEnv(t)
	installRuntime(t, env, mm)
	var v localruntime.View
	env.CallOK("get_local_runtime_status", map[string]any{"runtime_id": "gpt-sovits"}, &v)
	assert.True(t, v.Installed)
	assert.Equal(t, localruntime.WeightsReady, v.Weights.State)

	require.NoError(t, os.Remove(lr.WeightPath(lrtest.WeightDest)))
	env.CallOK("get_local_runtime_status", map[string]any{"runtime_id": "gpt-sovits"}, &v)
	assert.Equal(t, localruntime.WeightsMissing, v.Weights.State)
	assert.Contains(t, v.Weights.Message, "導入済み（重みが不足）")
	msg := env.CallError("start_local_runtime", map[string]any{"runtime_id": "gpt-sovits"})
	assert.Contains(t, msg, "conflict")
	assert.Contains(t, msg, "重みが不足しています（model）")

	var out modelmgr.JobOutput
	env.CallOK("install_local_runtime_weights", map[string]any{"runtime_id": "gpt-sovits"}, &out)
	assert.Equal(t, "runtime:gpt-sovits", out.Target)
	st := pollJob(t, env, out.JobID)
	require.Equal(t, mcpkit.JobSucceeded, st.State, st.Error)
	assert.Equal(t, "runtime:gpt-sovits", st.Result["target"])
	assert.FileExists(t, lr.WeightPath(lrtest.WeightDest))
}

func TestRuntimes_StartStopAutoStartAndRemove(t *testing.T) {
	env, mm, lr := newRuntimeEnv(t)
	assert.Contains(t, env.CallError("start_local_runtime", map[string]any{"runtime_id": "gpt-sovits"}), "導入されていません")
	assert.Contains(t, env.CallError("install_local_runtime_weights", map[string]any{"runtime_id": "gpt-sovits"}), "conflict")
	installRuntime(t, env, mm)

	var started modelmgr.RuntimeProcessOutput
	env.CallOK("start_local_runtime", map[string]any{"runtime_id": "gpt-sovits"}, &started)
	assert.Equal(t, "running", string(started.State))
	assert.Contains(t, started.URL, "http://127.0.0.1:")
	var stopped modelmgr.RuntimeProcessOutput
	env.CallOK("stop_local_runtime", map[string]any{"runtime_id": "gpt-sovits"}, &stopped)
	assert.Equal(t, "stopped", string(stopped.State))
	env.CallOK("stop_local_runtime", map[string]any{"runtime_id": "gpt-sovits"}, &stopped)

	var auto modelmgr.RuntimeAutoStartOutput
	env.CallOK("set_local_runtime_autostart", map[string]any{"runtime_id": "gpt-sovits", "on": true}, &auto)
	assert.True(t, auto.AutoStart)
	env.CallOK("set_local_runtime_autostart", map[string]any{"runtime_id": "gpt-sovits", "on": false}, &auto)
	assert.False(t, auto.AutoStart)

	var removed modelmgr.RuntimeRemovedOutput
	env.CallOK("remove_local_runtime", map[string]any{"runtime_id": "gpt-sovits"}, &removed)
	assert.True(t, removed.Removed)
	assert.NoFileExists(t, lr.WeightPath(lrtest.WeightDest))
	env.CallOK("remove_local_runtime", map[string]any{"runtime_id": "gpt-sovits"}, &removed)

	for _, tool := range []string{"stop_local_runtime", "remove_local_runtime", "start_local_runtime", "install_local_runtime", "install_local_runtime_weights"} {
		assert.Contains(t, env.CallError(tool, map[string]any{"runtime_id": "nope"}), "not_found", tool)
	}
	assert.Contains(t, env.CallError("set_local_runtime_autostart", map[string]any{"runtime_id": "nope", "on": true}), "not_found")
}

func TestRuntimes_CancelModelInstallStopsTheJobOfARuntimeToo(t *testing.T) {
	env, mm, lr := newRuntimeEnv(t)
	release := lr.Host.Hold(lrtest.WeightURLPath)
	defer release()
	var out modelmgr.JobOutput
	env.CallOK("install_local_runtime", map[string]any{"runtime_id": "gpt-sovits"}, &out)
	mm.Start()
	lr.Host.Reached(t)

	var again modelmgr.JobOutput
	env.CallOK("install_local_runtime", map[string]any{"runtime_id": "gpt-sovits"}, &again)
	assert.True(t, again.Existing, "the same install is not started twice")

	var c modelmgr.CancelOutput
	env.CallOK("cancel_model_install", map[string]any{"job_id": out.JobID}, &c)
	assert.True(t, c.Cancelled)
	st := pollJob(t, env, out.JobID)
	assert.Equal(t, mcpkit.JobFailed, st.State)
	assert.Contains(t, st.Error, "導入を中断しました")
	assert.Contains(t, env.CallError("cancel_model_install", map[string]any{"job_id": "nope"}), "not_found")
}

func TestRuntimes_UnavailableWithoutTheService(t *testing.T) {
	env, _ := newEnv(t)
	for _, tool := range []string{"list_local_runtimes"} {
		assert.Contains(t, env.CallError(tool, map[string]any{}), "unavailable", tool)
	}
	for _, tool := range []string{"get_local_runtime_status", "install_local_runtime", "install_local_runtime_weights", "start_local_runtime", "stop_local_runtime", "remove_local_runtime"} {
		assert.Contains(t, env.CallError(tool, map[string]any{"runtime_id": "gpt-sovits"}), "unavailable", tool)
	}
}
