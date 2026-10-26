package modelmgr_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/modelmgr"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/gpu"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
)

// jobDone polls get_job until the job is over.
func jobDone(t *testing.T, env *mcptest.Env, rig *mmtest.Rig, id string) mcpkit.JobStatus {
	t.Helper()
	rig.Start()
	var st mcpkit.JobStatus
	require.Eventually(t, func() bool {
		env.CallOK("get_job", map[string]any{"job_id": id}, &st)
		return st.State.Final()
	}, 30*time.Second, 20*time.Millisecond)
	return st
}

func TestEngine_InstallRunsAsAJobThatGetJobReadsAndTheStatusFollows(t *testing.T) {
	env, rig := newEnv(t)
	var before modelmanager.Status
	env.CallOK("get_model_engine_status", map[string]any{}, &before)
	assert.True(t, before.Engine.Supported)
	assert.False(t, before.Engine.Installed)
	assert.Equal(t, "v1.0.0", before.Engine.PinnedVersion)
	assert.Empty(t, before.Endpoints)

	var gp modelmgr.GPUOutput
	env.CallOK("detect_gpu", map[string]any{}, &gp)
	assert.Equal(t, "nvidia", string(gp.Recommendation.Variant))
	var cu modelmgr.ComfyOutput
	env.CallOK("detect_comfyui", map[string]any{}, &cu)
	assert.Empty(t, cu.Found)

	var out modelmgr.JobOutput
	env.CallOK("install_model_engine", map[string]any{}, &out)
	require.NotEmpty(t, out.JobID)
	assert.Equal(t, "engine", out.Target)
	assert.Equal(t, mcpkit.JobQueued, out.State)

	var st mcpkit.JobStatus
	env.CallOK("get_job", map[string]any{"job_id": out.JobID}, &st)
	assert.Equal(t, "model_install", st.Kind)
	assert.Equal(t, "engine", st.Target)
	assert.Contains(t, []mcpkit.JobState{mcpkit.JobQueued, mcpkit.JobRunning, mcpkit.JobSucceeded}, st.State)

	done := jobDone(t, env, rig, out.JobID)
	assert.Equal(t, mcpkit.JobSucceeded, done.State, done.Error)
	assert.Equal(t, 100, done.Done)
	assert.Equal(t, 100, done.Total)
	assert.Equal(t, "engine", done.Result["target"])

	var after modelmanager.Status
	env.CallOK("get_model_engine_status", map[string]any{}, &after)
	assert.True(t, after.Engine.Installed)
	require.Len(t, after.Endpoints, 1)
	assert.True(t, after.Endpoints[0].Bundled)

	// the id of the job table row is accepted by get_job as it is (after a restart the in-memory ids are gone)
	var byRow mcpkit.JobStatus
	env.CallOK("get_job", map[string]any{"job_id": out.JobID}, &byRow)
	assert.Equal(t, mcpkit.JobSucceeded, byRow.State)
}

func TestEngine_RefusalsAreToolErrors(t *testing.T) {
	t.Run("no supported GPU needs an explicit CPU choice", func(t *testing.T) {
		env, rig := newEnv(t)
		rig.GPUReport = gpu.Report{Recommendation: gpu.Recommendation{Guidance: "対応する GPU が見つかりません。外部の ComfyUI を使ってください", CPUAvailable: true}}
		msg := env.CallError("install_model_engine", map[string]any{})
		assert.Contains(t, msg, "validation")
		assert.Contains(t, msg, "対応する GPU が見つかりません")
		var out modelmgr.JobOutput
		env.CallOK("install_model_engine", map[string]any{"cpu": true}, &out)
		assert.NotEmpty(t, out.JobID)
	})
	t.Run("not Windows", func(t *testing.T) {
		env, _ := newEnv(t, mmtest.NotWindows())
		var st modelmanager.Status
		env.CallOK("get_model_engine_status", map[string]any{}, &st)
		assert.False(t, st.Engine.Supported)
		for _, tool := range []string{"install_model_engine", "start_model_engine"} {
			msg := env.CallError(tool, map[string]any{})
			assert.Contains(t, msg, "conflict", tool)
			assert.Contains(t, msg, "Windows 版だけ", tool)
		}
	})
	t.Run("unknown build", func(t *testing.T) {
		env, _ := newEnv(t)
		assert.Contains(t, env.CallError("install_model_engine", map[string]any{"variant": "quantum"}), "validation")
	})
	t.Run("start and cancel without anything to act on", func(t *testing.T) {
		env, _ := newEnv(t)
		assert.Contains(t, env.CallError("start_model_engine", map[string]any{}), "先に ComfyUI エンジンを導入してください")
		assert.Contains(t, env.CallError("cancel_model_install", map[string]any{"job_id": "nope"}), "not_found")
	})
	t.Run("unavailable without a model manager", func(t *testing.T) {
		env := mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) { modelmgr.Register(r, env.Services) })
		assert.Contains(t, env.CallError("get_model_engine_status", map[string]any{}), "unavailable")
		assert.Contains(t, env.CallError("list_model_catalog", map[string]any{}), "unavailable")
	})
}

func TestEngine_StartStopAutoStartAndRemove(t *testing.T) {
	env, rig := newEnv(t)
	rig.InstallEngine()

	var started modelmgr.ProcessOutput
	env.CallOK("start_model_engine", map[string]any{}, &started)
	assert.Equal(t, "running", string(started.State))
	assert.Contains(t, started.URL, "http://127.0.0.1:")
	var stopped modelmgr.ProcessOutput
	env.CallOK("stop_model_engine", map[string]any{}, &stopped)
	assert.Equal(t, "stopped", string(stopped.State))
	env.CallOK("stop_model_engine", map[string]any{}, &stopped)

	var auto modelmgr.AutoStartOutput
	env.CallOK("set_model_engine_autostart", map[string]any{"on": true}, &auto)
	assert.True(t, auto.AutoStart)
	env.CallOK("set_model_engine_autostart", map[string]any{"on": false}, &auto)
	assert.False(t, auto.AutoStart)

	var removed modelmgr.RemovedOutput
	env.CallOK("remove_model_engine", map[string]any{}, &removed)
	assert.True(t, removed.Removed)
	assert.False(t, rig.Svc.Engine().Installed)
}

func TestCatalog_ListsStateLicenseAndWhyNothingCanBeInstalled(t *testing.T) {
	env, rig := newEnv(t)
	var list modelmgr.ListCatalogOutput
	env.CallOK("list_model_catalog", map[string]any{}, &list)
	require.Len(t, list.Models, 3)
	assert.Contains(t, list.Blocked, "先に ComfyUI エンジンを導入してください")
	c := list.Models[2]
	assert.Equal(t, "m-c", c.ID)
	assert.True(t, c.ConsentRequired)
	assert.False(t, c.Accepted)
	assert.False(t, c.License.CommercialUse)
	assert.Equal(t, []string{"EU", "KR"}, c.License.ExcludedRegions)
	assert.Equal(t, 8, c.VRAM.MinGB)
	assert.Equal(t, "not_installed", string(c.State))

	rig.InstallEngine()
	var ready modelmgr.ListCatalogOutput
	env.CallOK("list_model_catalog", map[string]any{}, &ready)
	assert.Empty(t, ready.Blocked)

	var one modelmanager.ModelView
	env.CallOK("get_catalog_model", map[string]any{"model_id": "m-b"}, &one)
	assert.Equal(t, "m-b", one.ID)
	assert.Len(t, one.Files, 2)
	assert.Contains(t, env.CallError("get_catalog_model", map[string]any{"model_id": "nope"}), "not_found")
}

func TestInstallModel_ConsentIsEnforcedThroughMCPAndInstallsAfterAcceptance(t *testing.T) {
	env, rig := newEnv(t)
	rig.InstallEngine()
	requests := rig.Host.Requests()

	msg := env.CallError("install_model", map[string]any{"model_id": "m-c"})
	assert.Contains(t, msg, "conflict")
	assert.Contains(t, msg, "ライセンスに同意してから導入してください")
	assert.Equal(t, requests, rig.Host.Requests(), "nothing was downloaded")

	// the wrong license or version is refused and not recorded
	assert.Contains(t, env.CallError("accept_model_license", map[string]any{"model_id": "m-c", "license_id": "apache-2.0", "license_version": "1"}), "validation")
	assert.Contains(t, env.CallError("accept_model_license", map[string]any{"model_id": "m-c", "license_id": "non-commercial", "license_version": "9"}), "validation")
	assert.Contains(t, env.CallError("install_model", map[string]any{"model_id": "m-c", "license_id": "non-commercial", "license_version": "9"}), "validation")
	var one modelmanager.ModelView
	env.CallOK("get_catalog_model", map[string]any{"model_id": "m-c"}, &one)
	assert.False(t, one.Accepted)
	assert.Contains(t, env.CallError("accept_model_license", map[string]any{"model_id": "nope", "license_id": "x", "license_version": "1"}), "not_found")

	var acc modelmgr.AcceptOutput
	env.CallOK("accept_model_license", map[string]any{"model_id": "m-c", "license_id": "non-commercial", "license_version": "1"}, &acc)
	assert.True(t, acc.Accepted)

	var out modelmgr.JobOutput
	env.CallOK("install_model", map[string]any{"model_id": "m-c"}, &out)
	assert.Equal(t, "model:m-c", out.Target)
	st := jobDone(t, env, rig, out.JobID)
	assert.Equal(t, mcpkit.JobSucceeded, st.State, st.Error)
	assert.Equal(t, "model:m-c", st.Result["target"])
	assert.FileExists(t, rig.ModelFile(mmtest.ClipC))

	assert.Contains(t, env.CallError("install_model", map[string]any{"model_id": "m-c"}), "導入済み")
}

func TestInstallModel_AcceptanceCanComeWithTheCall(t *testing.T) {
	env, rig := newEnv(t)
	rig.InstallEngine()
	var out modelmgr.JobOutput
	env.CallOK("install_model", map[string]any{"model_id": "m-c", "license_id": "non-commercial", "license_version": "1"}, &out)
	st := jobDone(t, env, rig, out.JobID)
	assert.Equal(t, mcpkit.JobSucceeded, st.State, st.Error)
}

func TestInstallModel_RefusalsBeforeAnyDownload(t *testing.T) {
	t.Run("no engine", func(t *testing.T) {
		env, rig := newEnv(t)
		assert.Contains(t, env.CallError("install_model", map[string]any{"model_id": "m-a"}), "先に ComfyUI エンジンを導入してください")
		assert.Zero(t, rig.Host.Requests())
	})
	t.Run("unknown model", func(t *testing.T) {
		env, rig := newEnv(t)
		rig.InstallEngine()
		assert.Contains(t, env.CallError("install_model", map[string]any{"model_id": "nope"}), "not_found")
	})
	t.Run("no room", func(t *testing.T) {
		env, rig := newEnv(t)
		rig.InstallEngine()
		rig.Free = func(string) (uint64, error) { return 1000, nil }
		msg := env.CallError("install_model", map[string]any{"model_id": "m-a"})
		assert.Contains(t, msg, "空き容量が足りません")
	})
	t.Run("pinned model file", func(t *testing.T) {
		env, rig := newEnv(t, mmtest.Pinned())
		rig.InstallEngine()
		msg := env.CallError("install_model", map[string]any{"model_id": "m-a"})
		assert.Contains(t, msg, "conflict")
		assert.Contains(t, msg, "--model-config")
	})
}

func TestInstallModel_JobCanBeCancelledAndResumed(t *testing.T) {
	env, rig := newEnv(t)
	rig.InstallEngine()
	hold := rig.Host.Hold("/" + mmtest.CkptA.Folder + "/" + mmtest.CkptA.Name)
	var out modelmgr.JobOutput
	env.CallOK("install_model", map[string]any{"model_id": "m-a"}, &out)
	rig.Start()
	hold.Reached(t)

	var again modelmgr.JobOutput
	env.CallOK("install_model", map[string]any{"model_id": "m-a"}, &again)
	assert.True(t, again.Existing, "the same install does not start twice")
	assert.Equal(t, out.JobID, again.JobID)

	var c modelmgr.CancelOutput
	env.CallOK("cancel_model_install", map[string]any{"job_id": out.JobID}, &c)
	assert.True(t, c.Cancelled)
	hold.Release()
	st := jobDone(t, env, rig, out.JobID)
	assert.Equal(t, mcpkit.JobFailed, st.State)
	assert.Contains(t, st.Error, "導入を中断しました")
	assert.Equal(t, "not_installed", string(rig.Svc.Catalog()[0].State))

	var retry modelmgr.JobOutput
	env.CallOK("install_model", map[string]any{"model_id": "m-a"}, &retry)
	assert.Equal(t, mcpkit.JobSucceeded, jobDone(t, env, rig, retry.JobID).State)
}

func TestDeleteModel_KeepsSharedFilesWhileAnotherModelUsesThem(t *testing.T) {
	env, rig := newEnv(t)
	rig.InstallEngine()
	rig.InstallModel("m-b")
	rig.InstallModel("m-c")
	var del modelmgr.DeletedOutput
	env.CallOK("delete_model", map[string]any{"model_id": "m-b"}, &del)
	assert.True(t, del.Deleted)
	assert.NoFileExists(t, rig.ModelFile(mmtest.UnetB))
	assert.FileExists(t, rig.ModelFile(mmtest.VaeBC))
	assert.Contains(t, env.CallError("delete_model", map[string]any{"model_id": "m-b"}), "導入されていません")
	assert.Contains(t, env.CallError("delete_model", map[string]any{"model_id": "nope"}), "not_found")
}

func TestHuggingFaceToken_StatusOnly(t *testing.T) {
	env, rig := newEnv(t)
	const secret = "hf_abcdefghijklmnopqrstuvwxyz5678"
	var st modelmgr.TokenStatus
	env.CallOK("get_huggingface_token_status", map[string]any{}, &st)
	assert.False(t, st.Configured)

	for _, call := range []struct {
		tool string
		args map[string]any
	}{
		{"set_huggingface_token", map[string]any{"token": secret}},
		{"get_huggingface_token_status", map[string]any{}},
		{"set_huggingface_token", map[string]any{"token": "   "}},
	} {
		res := env.Call(call.tool, call.args)
		raw, err := json.Marshal(res)
		require.NoError(t, err)
		assert.NotContains(t, string(raw), secret, call.tool)
	}
	env.CallOK("get_huggingface_token_status", map[string]any{}, &st)
	assert.True(t, st.Configured)
	assert.Equal(t, "5678", st.LastChars)
	stored, err := rig.Keys.Get("huggingface")
	require.NoError(t, err)
	assert.Equal(t, secret, stored)

	env.CallOK("clear_huggingface_token", map[string]any{}, &st)
	assert.False(t, st.Configured)
	env.CallOK("clear_huggingface_token", map[string]any{}, &st)
	assert.True(t, strings.Contains(env.CallError("set_huggingface_token", map[string]any{"token": ""}), "validation"))
}

func TestExternalInventory_ReadsAnExternalComfyUIAndRefusesTheBundledOne(t *testing.T) {
	env, rig := newEnv(t)
	assert.Contains(t, env.CallError("get_external_inventory", map[string]any{"endpoint": "nope"}), "not_found")
	rig.InstallEngine()
	assert.Contains(t, env.CallError("get_external_inventory", map[string]any{"endpoint": "bundled-comfyui"}), "validation")
}
