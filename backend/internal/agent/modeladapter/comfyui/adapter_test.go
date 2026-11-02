package comfyui_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyuitest"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/configcheck"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localbackends"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/progress"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// A ComfyUI video model takes part in a Take like a built-in one: the Adapter renders the Shot Spec for its
// capabilities (lengths, first frame), calls it, reports the progress and names it by its identifier.
func TestVideoModelThroughTheAdapter(t *testing.T) {
	f := comfyuitest.New(t)
	f.OutputFile, f.OutputNode, f.OutputBody = "clip.mp4", "58", []byte("mp4")
	cfg := modeladapter.DefaultConfig()
	cfg.Endpoints = map[string]localcfg.Endpoint{"box": {Kind: localcfg.KindComfyUI, BaseURL: f.URL()}}
	tpl, _ := localcfg.TemplateByID("img2video-wan22-5b")
	m := tpl.Model
	m.CostPerSecond = 0.02
	cfg.LocalModels = map[string]localcfg.Model{"box/wan": m}
	require.NoError(t, configcheck.Validate(cfg))
	a := modeladapter.New(cfg, localbackends.Build(cfg, credentialtest.New(), localbackends.Options{PollEvery: 5 * time.Millisecond, StagingDir: t.TempDir()}))

	target := modeladapter.VideoTarget{Model: "comfyui:box/wan", Resolution: "480p"}
	require.Empty(t, a.Unavailable(target.Model))
	var got updates
	job := modeladapter.VideoJob{
		Spec:            domain.ShotSpec{ShotObjective: "A fox runs"},
		DurationSeconds: 4.2, AspectRatio: "9:16",
		FirstFrame: &modeladapter.Input{Data: comfyuitest.PNG, MimeType: "image/png"},
		References: []modeladapter.Input{{Data: comfyuitest.PNG, MimeType: "image/png"}},
	}
	out, err := a.GenerateVideo(progress.With(ctx(t), got.add), target, target, job)
	require.NoError(t, err)
	defer out.File.Discard()

	assert.Equal(t, domain.ModelName("comfyui:box/wan"), out.Model)
	assert.Equal(t, 5.0, out.DurationSeconds, "snapped to the model's own lengths (3, 4, 5)")
	assert.InDelta(t, 0.10, out.EstimatedCost, 1e-9)
	assert.Nil(t, out.Request.References, "a workflow with no reference_image assignment takes the first frame only")
	require.NotNil(t, out.Request.FirstFrame)
	g := f.Graph()
	assert.Equal(t, 121.0, num(g["55"].Inputs["length"]))
	assert.Equal(t, 480.0, num(g["55"].Inputs["width"]), "9:16 at 480p: the short side is the width")
	assert.Equal(t, 864.0, num(g["55"].Inputs["height"]))
	assert.NotEmpty(t, got.all())
}

// A local video that runs past the endpoint's timeout is not left running on the GPU: the job is cancelled on
// the server by its own ID, the error says it was a local call (nothing is billed), and the slot of the endpoint
// is free again, so the job behind it starts.
func TestLocalVideoPastTheTimeoutIsCancelledOnTheServerAndFreesTheSlot(t *testing.T) {
	f := comfyuitest.New(t)
	f.OutputFile, f.OutputNode, f.OutputBody = "clip.mp4", "58", []byte("mp4")
	f.Hold = make(chan struct{})
	cfg := modeladapter.DefaultConfig()
	cfg.Endpoints = map[string]localcfg.Endpoint{"box": {Kind: localcfg.KindComfyUI, BaseURL: f.URL(), TimeoutSeconds: 1}}
	tpl, _ := localcfg.TemplateByID("img2video-wan22-5b")
	cfg.LocalModels = map[string]localcfg.Model{"box/wan": tpl.Model}
	require.NoError(t, configcheck.Validate(cfg))
	a := modeladapter.New(cfg, localbackends.Build(cfg, credentialtest.New(), localbackends.Options{PollEvery: 5 * time.Millisecond, StagingDir: t.TempDir()}))
	target := modeladapter.VideoTarget{Model: "comfyui:box/wan", Resolution: "480p"}
	job := modeladapter.VideoJob{
		Spec: domain.ShotSpec{ShotObjective: "A fox runs"}, DurationSeconds: 5, AspectRatio: "16:9",
		FirstFrame: &modeladapter.Input{Data: comfyuitest.PNG, MimeType: "image/png"},
	}

	_, err := a.GenerateVideo(ctx(t), target, target, job)

	require.ErrorIs(t, err, modeladapter.ErrTimeout)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	var te *localcfg.TimeoutError
	require.ErrorAs(t, err, &te)
	assert.Equal(t, time.Second, te.After)
	require.Eventually(t, func() bool { return f.Count("POST /api/jobs/") == 1 }, 5*time.Second, 5*time.Millisecond)
	assert.Equal(t, str(f.Submitted()[0]["prompt_id"]), f.Cancelled()[0], "the timed-out job is cancelled on the server")

	close(f.Hold)
	out, err := a.GenerateVideo(ctx(t), target, target, job)
	require.NoError(t, err, "the next job starts after the timeout")
	out.File.Discard()
}
