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

func TestBeginToolRemovesWhatAnEndedMamLeftInTheWorkDirButNothingOutsideIt(t *testing.T) {
	r := newRig(t)
	r.install()
	train := filepath.Join(r.layout.Root, "train")
	stale := filepath.Join(train, "old-run")
	require.NoError(t, os.MkdirAll(filepath.Join(stale, "exp"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(stale, "exp", "big.bin"), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(train, "loose.log"), []byte("x"), 0o600))
	outside := filepath.Join(t.TempDir(), "keep")
	require.NoError(t, os.MkdirAll(outside, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "file"), []byte("x"), 0o600))
	if err := os.Symlink(outside, filepath.Join(train, "link")); err != nil {
		t.Skipf("no symbolic link here: %v", err)
	}

	tool, err := r.svc.BeginTool(ctx(), "gpt-sovits")

	require.NoError(t, err)
	assert.NoDirExists(t, stale)
	assert.NoFileExists(t, filepath.Join(train, "loose.log"))
	assert.NoDirExists(t, filepath.Join(train, "link"), "the link is gone")
	assert.FileExists(t, filepath.Join(outside, "file"), "what the link pointed at is not followed")
	assert.DirExists(t, r.layout.EnvDir(), "nothing but the contents of the work directory goes")

	// The directory of a job that runs is not the leftover of one: a second job is refused before it cleans.
	live := filepath.Join(tool.Dir, "live")
	require.NoError(t, os.MkdirAll(live, 0o755))
	_, err = r.svc.BeginTool(ctx(), "gpt-sovits")
	assert.ErrorIs(t, err, localruntime.ErrTraining)
	assert.DirExists(t, live)
	tool.End()
}

func TestBeginToolDoesNotFollowAWorkDirThatIsALink(t *testing.T) {
	r := newRig(t)
	r.install()
	outside := filepath.Join(t.TempDir(), "keep")
	require.NoError(t, os.MkdirAll(outside, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "file"), []byte("x"), 0o600))
	if err := os.Symlink(outside, filepath.Join(r.layout.Root, "train")); err != nil {
		t.Skipf("no symbolic link here: %v", err)
	}

	tool, err := r.svc.BeginTool(ctx(), "gpt-sovits")

	require.NoError(t, err)
	defer tool.End()
	assert.FileExists(t, filepath.Join(outside, "file"), "the contents of the target are not removed")
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
