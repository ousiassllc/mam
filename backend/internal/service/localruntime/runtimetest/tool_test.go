package runtimetest_test

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
)

func TestBeginToolHoldsTheRuntimeStopsTheServerAndGivesItBack(t *testing.T) {
	r := newRig(t)
	r.install()
	_, err := r.svc.Start(ctx(), "gpt-sovits")
	require.NoError(t, err)

	tool, err := r.svc.BeginTool(ctx(), "gpt-sovits")

	require.NoError(t, err)
	v := r.view(t)
	assert.Equal(t, proc.StateStopped, v.Process.State, "the server and the training never share the GPU")
	assert.True(t, r.svc.Holding("gpt-sovits"))
	assert.Equal(t, filepath.Join(r.layout.Root, "train"), tool.Dir)
	assert.Equal(t, "NVIDIA GeForce RTX 4070", tool.GPUName)
	assert.Equal(t, 12, tool.VRAMGB)

	_, err = r.svc.BeginTool(ctx(), "gpt-sovits")
	assert.ErrorIs(t, err, localruntime.ErrTraining, "one job holds it")
	_, err = r.svc.Install(ctx(), "gpt-sovits")
	assert.ErrorIs(t, err, localruntime.ErrTraining, "no install under the files the job runs")
	_, err = r.svc.InstallWeights(ctx(), "gpt-sovits")
	assert.ErrorIs(t, err, localruntime.ErrTraining)

	tool.End()
	tool.End() // twice is the same as once
	assert.False(t, r.svc.Holding("gpt-sovits"))
	_, err = r.svc.Start(ctx(), "gpt-sovits")
	assert.NoError(t, err, "the server starts again once the job is over")
}

func TestBeginToolRefusesWithTheReasonOfTheRuntime(t *testing.T) {
	t.Run("not installed", func(t *testing.T) {
		r := newRig(t)
		_, err := r.svc.BeginTool(ctx(), "gpt-sovits")
		assert.ErrorIs(t, err, localruntime.ErrNotInstalled)
		assert.False(t, r.svc.Holding("gpt-sovits"))
	})
	t.Run("a machine that cannot run it", func(t *testing.T) {
		r := newRig(t, platform("linux", "amd64"))
		_, err := r.svc.BeginTool(ctx(), "gpt-sovits")
		assert.ErrorIs(t, err, localruntime.ErrUnsupported)
	})
	t.Run("too little video memory", func(t *testing.T) {
		r := newRig(t, withGPU(rtx("NVIDIA GeForce GTX 1050", 2, 6.1)))
		_, err := r.svc.BeginTool(ctx(), "gpt-sovits")
		assert.ErrorIs(t, err, localruntime.ErrUnsupported)
	})
	t.Run("an unknown runtime", func(t *testing.T) {
		r := newRig(t)
		_, err := r.svc.BeginTool(ctx(), "nope")
		assert.ErrorIs(t, err, localruntime.ErrNotFound)
	})
}

func TestBeginToolWantsTheWeightsThatOnlyTheJobReads(t *testing.T) {
	r := newRig(t, withWeights())
	r.install() // the package and then every weight of the catalog, the optional one too

	tool, err := r.svc.BeginTool(ctx(), "gpt-sovits", "extra")
	require.NoError(t, err, "a weight that is there and verified")
	tool.End()

	require.NoError(t, os.Remove(r.weightPath(extraDest)))
	_, err = r.svc.BeginTool(ctx(), "gpt-sovits", "extra")
	var we *localruntime.WeightsError
	require.ErrorAs(t, err, &we, "an optional weight that is gone stops the job that needs it")
	assert.Equal(t, "extra", we.Assets[0].Name)
	assert.False(t, r.svc.Holding("gpt-sovits"), "nothing is held after a refusal")

	_, err = r.svc.BeginTool(ctx(), "gpt-sovits", "no such asset")
	assert.ErrorAs(t, err, &we, "a name the catalog does not have is missing too")
	tool, err = r.svc.BeginTool(ctx(), "gpt-sovits")
	require.NoError(t, err, "the server itself does not need it")
	tool.End()
}

func TestToolSpecLaunchesOnlyAToolFileInTheEnvironmentOfTheRuntime(t *testing.T) {
	r := newRig(t)
	r.install()
	tool, err := r.svc.BeginTool(ctx(), "gpt-sovits")
	require.NoError(t, err)
	defer tool.End()

	spec, err := tool.Spec("mam_gptsovits_train.py", []string{"text", "job.json"}, []string{"X=1"})

	require.NoError(t, err)
	assert.Equal(t, r.layout.EnvDir(), spec.Dir)
	assert.Equal(t, []string{"-I", "-u", "-X", "utf8", "mam_gptsovits_train.py", "text", "job.json"}, spec.Args)
	assert.Contains(t, spec.Env, "X=1")
	assert.Contains(t, spec.Env, "HF_HUB_OFFLINE=1", "offline like the server")
	assert.True(t, slices.ContainsFunc(spec.Env, func(e string) bool { return len(e) > 8 && e[:8] == "HF_HOME=" }))
	assert.FileExists(t, filepath.Join(r.layout.EnvDir(), "mam_gptsovits_train.py"), "the script is written before it runs")

	for _, script := range []string{"api_v2.py", "../x.py", "webui.py", ""} {
		_, err := tool.Spec(script, nil, nil)
		assert.Error(t, err, "%q is no tool file", script)
	}
}

func TestEnvIsAddedToTheStartOfTheServer(t *testing.T) {
	r := newRig(t, func(_ *rig, d *localruntime.Deps) {
		d.Env = func(id string) []string { return []string{"MAM_VOICE_WEIGHTS_DIR=/data/" + id} }
	})
	r.install()

	st, err := r.svc.Start(ctx(), "gpt-sovits")
	require.NoError(t, err)

	res, err := http.Get(st.URL + "/env?name=MAM_VOICE_WEIGHTS_DIR")
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	body, _ := io.ReadAll(res.Body)
	assert.Equal(t, "/data/gpt-sovits", string(body))
}
