package musetalktest_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/httpclient"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/lipsync"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	rtm "github.com/ousiassllc/mam/backend/internal/modelmgr/runtime"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
)

func TestMuseTalkIsInstalledFromItsPartsWithItsWeightsAndStartsAndStops(t *testing.T) {
	r := newRig(t)
	before, err := r.svc.Status(ctx(), "musetalk")
	require.NoError(t, err)
	assert.Equal(t, catalog.PurposeLipSync, before.Purpose)
	assert.True(t, before.Support.Supported)
	assert.False(t, before.Installed)
	require.Len(t, before.Variants, 1)
	assert.Equal(t, localruntime.WeightsNone, before.Weights.State)

	r.installMuseTalk()
	v, err := r.svc.Status(ctx(), "musetalk")
	require.NoError(t, err)
	assert.True(t, v.Installed)
	assert.Equal(t, "test", v.Version)
	assert.Equal(t, localruntime.WeightsReady, v.Weights.State, "the weights are fetched by the same job")
	env := filepath.Join(r.root, "musetalk", "env")
	for _, f := range []string{"python/python.exe", "scripts/inference.py", "python/Lib/site-packages/torch/__init__.py", "mam_musetalk_server.py", weightDest} {
		assert.FileExists(t, filepath.Join(env, filepath.FromSlash(f)), f)
	}

	st, err := r.svc.Start(ctx(), "musetalk")
	require.NoError(t, err)
	assert.Equal(t, proc.StateRunning, st.State)
	require.NoError(t, r.svc.Stop("musetalk"))
	v, _ = r.svc.Status(ctx(), "musetalk")
	assert.Equal(t, proc.StateStopped, v.Process.State)
}

func TestLipSyncStartsTheRuntimeLazilyAndReturnsTheReport(t *testing.T) {
	r := newRig(t)
	r.installMuseTalk()
	v, _ := r.svc.Status(ctx(), "musetalk")
	require.Equal(t, proc.StateStopped, v.Process.State)

	req := inputs(t, "face.mp4")
	res, err := r.svc.LipSync(ctx(), req)
	require.NoError(t, err, "the token of the start reaches the server: it answers 403 to any other")
	assert.Equal(t, lipsync.Response{OutputPath: req.OutputPath, Frames: 25, FPS: 25, DurationSec: 1, ElapsedSec: 0.5}, res)
	got, err := os.ReadFile(req.OutputPath)
	require.NoError(t, err)
	assert.Equal(t, "fake-mp4", string(got))
	v, _ = r.svc.Status(ctx(), "musetalk")
	assert.Equal(t, proc.StateRunning, v.Process.State, "the first call started the server")

	// A client with another token is refused by the server itself: the token is what lets mam's calls in.
	c := lipsync.Client{BaseURL: "http://127.0.0.1:" + strconv.Itoa(v.Process.Port), Token: "not-the-token-of-this-start"}
	_, err = c.Lipsync(ctx(), req)
	require.ErrorIs(t, err, lipsync.ErrForbidden)

	// The second call uses the running server.
	_, err = r.svc.LipSync(ctx(), req)
	require.NoError(t, err)
}

// Service.Token is the token of the current start, which the server accepts (the voice client takes it from here for GPT-SoVITS).
func TestTokenIsTheOneOfTheCurrentStartAndTheServerAcceptsIt(t *testing.T) {
	r := newRig(t)
	r.installMuseTalk()
	_, err := r.svc.Token("nope")
	require.ErrorIs(t, err, localruntime.ErrNotFound)
	req := inputs(t, "face.mp4")
	_, err = r.svc.LipSync(ctx(), req)
	require.NoError(t, err)
	v, _ := r.svc.Status(ctx(), "musetalk")

	token, err := r.svc.Token("musetalk")

	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(token), 16)
	_, err = lipsync.Client{BaseURL: "http://127.0.0.1:" + strconv.Itoa(v.Process.Port), Token: token}.Lipsync(ctx(), req)
	require.NoError(t, err)
}

func TestLipSyncReportsWhatTheGenerationRefusedWith(t *testing.T) {
	r := newRig(t)
	r.installMuseTalk()
	_, err := r.svc.LipSync(ctx(), inputs(t, "noface.mp4"))
	require.ErrorIs(t, err, lipsync.ErrNoFace)
	assert.Contains(t, err.Error(), "顔を見つけられません")
	_, err = r.svc.LipSync(ctx(), inputs(t, "oom.mp4"))
	require.ErrorIs(t, err, lipsync.ErrOutOfMemory)

	v, _ := r.svc.Status(ctx(), "musetalk")
	assert.Equal(t, proc.StateRunning, v.Process.State, "a refused generation does not stop the runtime")
}

// Nothing is started for a request the runtime cannot serve: the refusal comes with the sentence and the cloud provider to use.
func TestLipSyncRefusesBeforeStartingAnythingWhereTheRuntimeCannotRun(t *testing.T) {
	t.Run("not installed", func(t *testing.T) {
		r := newRig(t)
		_, err := r.svc.LipSync(ctx(), inputs(t, "face.mp4"))
		require.ErrorIs(t, err, localruntime.ErrNotInstalled)
	})
	t.Run("a weight is missing", func(t *testing.T) {
		r := newRig(t)
		r.installMuseTalk()
		require.NoError(t, os.Remove(filepath.Join(r.root, "musetalk", "env", filepath.FromSlash(weightDest))))
		_, err := r.svc.LipSync(ctx(), inputs(t, "face.mp4"))
		require.ErrorIs(t, err, localruntime.ErrWeightsMissing)
		assert.Contains(t, err.Error(), "MuseTalk 1.5 の重みが不足しています")
		v, _ := r.svc.Status(ctx(), "musetalk")
		assert.Equal(t, proc.StateStopped, v.Process.State)
	})
	t.Run("macOS", func(t *testing.T) {
		r := newRig(t, platform("darwin", "arm64"))
		_, err := r.svc.LipSync(ctx(), inputs(t, "face.mp4"))
		require.ErrorIs(t, err, localruntime.ErrUnsupported)
		var se *localruntime.SupportError
		require.ErrorAs(t, err, &se)
		assert.Equal(t, rtm.CodeOS, se.Support.Code)
		assert.Contains(t, err.Error(), "macOS では MuseTalk 1.5 を自動導入できません")
		assert.Contains(t, err.Error(), "Sync Labs")
	})
	t.Run("an RTX 50 series GPU", func(t *testing.T) {
		r := newRig(t, withGPU(rtx("NVIDIA GeForce RTX 5090", 32, 12.0)))
		_, err := r.svc.Install(ctx(), "musetalk")
		var se *localruntime.SupportError
		require.ErrorAs(t, err, &se)
		assert.Equal(t, rtm.CodeComputeCap, se.Support.Code)
		assert.Contains(t, err.Error(), "RTX 50 シリーズ")
	})
	t.Run("a GPU with too little memory", func(t *testing.T) {
		r := newRig(t, withGPU(rtx("NVIDIA GeForce GT 1030", 2, 6.1)))
		_, err := r.svc.Install(ctx(), "musetalk")
		var se *localruntime.SupportError
		require.ErrorAs(t, err, &se)
		assert.Equal(t, rtm.CodeLowVRAM, se.Support.Code)
	})
	t.Run("a request that is wrong", func(t *testing.T) {
		r := newRig(t)
		r.installMuseTalk()
		bad := inputs(t, "face.mp4")
		bad.OutputPath = bad.VideoPath
		_, err := r.svc.LipSync(ctx(), bad)
		require.ErrorIs(t, err, lipsync.ErrBadRequest)
		v, _ := r.svc.Status(ctx(), "musetalk")
		assert.Equal(t, proc.StateStopped, v.Process.State, "a wrong request starts nothing")
	})
	t.Run("no lip-sync runtime in the catalog", func(t *testing.T) {
		r := newRig(t, func(_ *rig, d *localruntime.Deps) { d.Catalog = &catalog.Catalog{} })
		_, err := r.svc.LipSync(ctx(), inputs(t, "face.mp4"))
		require.ErrorIs(t, err, localruntime.ErrNotFound)
	})
}

// blockingDoer holds the first request until it is released, so that the test sees the second one wait.
type blockingDoer struct {
	mu      sync.Mutex
	calls   int
	entered chan struct{}
	release chan struct{}
}

func (d *blockingDoer) Do(*http.Request) (*http.Response, error) {
	d.mu.Lock()
	d.calls++
	n := d.calls
	d.mu.Unlock()
	if n == 1 {
		d.entered <- struct{}{}
		<-d.release
	}
	rec := httptest.NewRecorder()
	rec.WriteHeader(http.StatusOK)
	_, _ = rec.WriteString(`{"output_path":"/x/out.mp4","frames":1,"fps":25}`)
	return rec.Result(), nil
}

var _ httpclient.Doer = (*blockingDoer)(nil)

// The GPU is one: a second generation waits for the first, and gives up with its own context.
func TestLipSyncRunsOneGenerationAtATime(t *testing.T) {
	d := &blockingDoer{entered: make(chan struct{}, 1), release: make(chan struct{})}
	r := newRig(t, func(_ *rig, deps *localruntime.Deps) { deps.HTTP = d })
	r.installMuseTalk()

	firstDone := make(chan error, 1)
	go func() { _, err := r.svc.LipSync(ctx(), inputs(t, "face.mp4")); firstDone <- err }()
	select {
	case <-d.entered:
	case <-time.After(20 * time.Second):
		t.Fatal("the first generation never reached the server")
	}

	waiting, cancel := context.WithCancel(ctx())
	secondDone := make(chan error, 1)
	go func() { _, err := r.svc.LipSync(waiting, inputs(t, "face.mp4")); secondDone <- err }()
	cancel()
	require.ErrorIs(t, <-secondDone, context.Canceled, "the waiting call ends with its context")
	d.mu.Lock()
	assert.Equal(t, 1, d.calls, "the second request was never sent while the first one ran")
	d.mu.Unlock()

	close(d.release)
	require.NoError(t, <-firstDone)
	_, err := r.svc.LipSync(ctx(), inputs(t, "face.mp4"))
	require.NoError(t, err, "the slot is free again")
}
