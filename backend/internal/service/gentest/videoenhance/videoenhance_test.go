package videoenhance_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/cloudenhance"
	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/videoenhance"
)

var ctx = context.Background()

const (
	proteus = videoenhance.CloudPrefix + "prob-4"
	apollo  = videoenhance.CloudPrefix + "apo-8"
)

func newCloud(configured bool) *gentesttest.FakeCloud {
	return &gentesttest.FakeCloud{
		IsConfigured: configured, Result: []byte("TOPAZ"),
		ModelList: []cloudenhance.CloudModel{
			{ID: "prob-4", Provider: cloudenhance.ProviderTopaz, Op: cloudenhance.OpUpscale, Label: "Proteus"},
			{ID: "apo-8", Provider: cloudenhance.ProviderTopaz, Op: cloudenhance.OpInterpolate, Label: "Apollo"},
		},
		Quote: cloudenhance.CloudEstimate{RequestID: "req-1", Credits: 12.5, CreditsMin: 10, Width: 960, Height: 1696, FPS: 16},
	}
}

func newRig(t *testing.T, opts ...gentesttest.Option) *gentesttest.Rig {
	t.Helper()
	opts = append([]gentesttest.Option{gentesttest.WithVideoEnhanceModels(), gentesttest.WithProbe(gentesttest.FakeProbe{Info: gentesttest.A480p16})}, opts...)
	return gentesttest.New(t, opts...)
}

// aVideo makes a succeeded video result and returns its id.
func aVideo(t *testing.T, r *gentesttest.Rig) string {
	t.Helper()
	ref, err := r.Confirmed(gentest.Request{Media: "video", Models: []string{string(domain.ModelKling3)}, Prompts: []gentest.PromptInput{{Prompt: "a fox runs"}}, DurationSeconds: 5})
	require.NoError(t, err)
	st := r.Wait(ref.BatchID)
	require.Equal(t, 1, st.Succeeded, st.Results)
	return st.Results[0].ID
}

// enhance starts the enhancement and returns the finished result.
func enhance(t *testing.T, r *gentesttest.Rig, req videoenhance.Request) record.ResultView {
	t.Helper()
	ref, err := videoenhance.Enhance(ctx, r.Svc, req)
	require.NoError(t, err)
	st := r.Wait(ref.BatchID)
	require.Len(t, st.Results, 1)
	assert.Equal(t, ref.ResultID, st.Results[0].ID)
	return st.Results[0]
}

func invalidMessage(t *testing.T, err error) string {
	t.Helper()
	var inv *gterr.InvalidError
	require.ErrorAs(t, err, &inv)
	return inv.Message
}

func conflictMessage(t *testing.T, err error) string {
	t.Helper()
	var c *gterr.ConflictError
	require.ErrorAs(t, err, &c)
	return c.Message
}

func upscale(id, model string) videoenhance.Request {
	return videoenhance.Request{ResultID: id, Op: cloudenhance.OpUpscale, Model: model, Scale: 2}
}

func TestLocalUpscaleMakesAResultThatRemembersItsSource(t *testing.T) {
	r := newRig(t)
	src := aVideo(t, r)

	res := enhance(t, r, upscale(src, string(gentesttest.UpscaleVideoModel)))

	require.Equal(t, "succeeded", res.Status, res.Error)
	require.NotNil(t, res.VideoEnhance)
	assert.Equal(t, src, res.VideoEnhance.SourceResultID)
	assert.Equal(t, string(domain.ModelKling3), res.VideoEnhance.SourceModel)
	assert.Equal(t, "upscale", res.VideoEnhance.Op)
	assert.Equal(t, "local", res.VideoEnhance.Provider)
	assert.Equal(t, string(gentesttest.UpscaleVideoModel), res.VideoEnhance.Model)
	assert.InDelta(t, 2, res.VideoEnhance.Scale, 0)
	assert.Equal(t, [3]int{480, 848, 81}, [3]int{res.VideoEnhance.SourceWidth, res.VideoEnhance.SourceHeight, res.VideoEnhance.SourceFrames}, "the facts come from the file")
	assert.Equal(t, [2]int{960, 1696}, [2]int{res.VideoEnhance.Width, res.VideoEnhance.Height})
	assert.Equal(t, modelcatalog.BasisFree, res.CostBasis, "a local model is free")
	assert.Nil(t, res.VideoEnhance.Topaz)

	calls := r.Fake.CallsOf(gentesttest.UpscaleVideoModel)
	require.Len(t, calls, 1)
	assert.Equal(t, "MP4DATA", string(calls[0].Enhance.Source.Data), "the model got the file of the source result")
	assert.InDelta(t, 2, calls[0].Enhance.Scale, 0)
	file, err := r.Svc.ResultFile(ctx, res.ID)
	require.NoError(t, err)
	data, err := os.ReadFile(file.Path)
	require.NoError(t, err)
	assert.Equal(t, "ENHANCED", string(data), "the result is a file of its own")
	got, err := r.Svc.Result(ctx, res.ID)
	require.NoError(t, err)
	assert.Equal(t, src, got.VideoEnhance.SourceResultID, "the history shows it too")
}

func TestUpscaleByShortSideAndChaining(t *testing.T) {
	r := newRig(t)
	src := aVideo(t, r)

	res := enhance(t, r, videoenhance.Request{ResultID: src, Op: cloudenhance.OpUpscale, TargetShortSide: 720})
	assert.Equal(t, [2]int{720, 1272}, [2]int{res.VideoEnhance.Width, res.VideoEnhance.Height}, "480 × 848 at 720 ends 720 × 1272")
	assert.Equal(t, string(gentesttest.UpscaleVideoModel), res.VideoEnhance.Model, "no model named: the first local one that can run")

	again := enhance(t, r, videoenhance.Request{ResultID: res.ID, Op: cloudenhance.OpInterpolate, Multiplier: 2})
	require.Equal(t, "succeeded", again.Status, again.Error)
	assert.Equal(t, res.ID, again.VideoEnhance.SourceResultID, "a result of an enhancement may be enhanced")
	assert.InDelta(t, 32, again.VideoEnhance.FPS, 0)
}

func TestInterpolateOnlyMakesWholeMultiplesLocally(t *testing.T) {
	r := newRig(t)
	src := aVideo(t, r)
	model := string(gentesttest.InterpolateVideoModel)

	_, err := videoenhance.Enhance(ctx, r.Svc, videoenhance.Request{ResultID: src, Op: cloudenhance.OpInterpolate, Model: model, TargetFPS: 24})
	msg := invalidMessage(t, err)
	assert.Contains(t, msg, "整数倍")
	assert.Contains(t, msg, "Topaz")
	assert.Empty(t, r.Fake.CallsOf(gentesttest.InterpolateVideoModel))

	res := enhance(t, r, videoenhance.Request{ResultID: src, Op: cloudenhance.OpInterpolate, Model: model, TargetFPS: 64})
	require.Equal(t, "succeeded", res.Status, res.Error)
	assert.Equal(t, 4, r.Fake.CallsOf(gentesttest.InterpolateVideoModel)[0].Enhance.Multiplier, "64 fps from 16 fps is 4x")
	assert.InDelta(t, 64, r.Fake.CallsOf(gentesttest.InterpolateVideoModel)[0].Enhance.OutputFPS, 0)
}

func TestOnlyASucceededVideoIsEnhanced(t *testing.T) {
	r := newRig(t)
	ref, err := r.Confirmed(gentest.Request{Media: "image", Models: []string{string(domain.ModelFlux2)}, Prompts: []gentest.PromptInput{{Prompt: "a fox"}}})
	require.NoError(t, err)
	image := r.Wait(ref.BatchID).Results[0].ID

	_, err = videoenhance.Enhance(ctx, r.Svc, upscale(image, ""))
	assert.Contains(t, invalidMessage(t, err), "成功した動画ではありません")

	_, err = videoenhance.Enhance(ctx, r.Svc, upscale("nope", ""))
	assert.ErrorIs(t, err, gterr.ErrNotFound)
}

func TestWithoutFfprobeTheEnhancementIsRefusedWithTheFix(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithVideoEnhanceModels()) // no Probe: no ffmpeg
	src := aVideo(t, r)

	_, err := videoenhance.Enhance(ctx, r.Svc, upscale(src, ""))

	e, ok := apperr.As(err)
	require.True(t, ok, "the common error: %v", err)
	assert.Equal(t, apperr.CodeFFmpegMissing, e.Code)
	v := e.View(apperr.LangJa)
	assert.Contains(t, v.Cause, "ffmpeg")
	assert.NotEmpty(t, v.Remedy, "it says where to set ffmpeg")
}

func TestBadValuesAreRefused(t *testing.T) {
	r := newRig(t)
	src := aVideo(t, r)
	for name, req := range map[string]videoenhance.Request{
		"no size":          {ResultID: src, Op: cloudenhance.OpUpscale},
		"both":             {ResultID: src, Op: cloudenhance.OpUpscale, Scale: 2, TargetShortSide: 720},
		"scale 1":          {ResultID: src, Op: cloudenhance.OpUpscale, Scale: 1},
		"scale over 4":     {ResultID: src, Op: cloudenhance.OpUpscale, Scale: 4.5},
		"short side under": {ResultID: src, Op: cloudenhance.OpUpscale, TargetShortSide: 360},
		"multiplier 3":     {ResultID: src, Op: cloudenhance.OpInterpolate, Multiplier: 3},
		"fps below source": {ResultID: src, Op: cloudenhance.OpInterpolate, TargetFPS: 8, Model: string(gentesttest.InterpolateVideoModel)},
		"mixed":            {ResultID: src, Op: cloudenhance.OpUpscale, Scale: 2, Multiplier: 2},
		"unknown op":       {ResultID: src, Op: "rotate", Scale: 2},
		"wrong model":      {ResultID: src, Op: cloudenhance.OpInterpolate, Multiplier: 2, Model: string(gentesttest.UpscaleVideoModel)},
		"no such model":    {ResultID: src, Op: cloudenhance.OpUpscale, Scale: 2, Model: "comfyui:nope/x"},
	} {
		_, err := videoenhance.Enhance(ctx, r.Svc, req)
		var inv *gterr.InvalidError
		assert.ErrorAs(t, err, &inv, name)
	}
	assert.Empty(t, enhanceCalls(r), "nothing was run")
}

// The upscale workflow of SeedVR2 makes at most 1920 × 1080 pixels per frame (its own limit in the template); the cloud takes what the encoder takes.
func TestOutputOverTheModelsPixelLimitIsRefused(t *testing.T) {
	cloud := newCloud(true)
	r := newRig(t, gentesttest.WithCloud(cloud))
	src := aVideo(t, r)

	_, err := videoenhance.Enhance(ctx, r.Svc, videoenhance.Request{ResultID: src, Op: cloudenhance.OpUpscale, Scale: 4, Model: string(gentesttest.UpscaleVideoModel)})
	msg := invalidMessage(t, err)
	assert.Contains(t, msg, "1920 × 3392")
	assert.Contains(t, msg, "上限")

	_, err = videoenhance.EstimateEnhance(ctx, r.Svc, videoenhance.Request{ResultID: src, Op: cloudenhance.OpUpscale, Scale: 4, Model: proteus})
	assert.NoError(t, err, "the same size is fine for the cloud: under 3840 × 2160 pixels")

	huge := newRig(t, gentesttest.WithCloud(cloud), gentesttest.WithProbe(gentesttest.FakeProbe{Info: gentesttest.A1080p24}))
	big := aVideo(t, huge)
	_, err = videoenhance.EstimateEnhance(ctx, huge.Svc, videoenhance.Request{ResultID: big, Op: cloudenhance.OpUpscale, Scale: 3, Model: proteus})
	assert.Contains(t, invalidMessage(t, err), "上限", "5760 × 3240 is over the limit of every model")
}

func TestLocalEstimateIsFreeWithoutAnId(t *testing.T) {
	r := newRig(t)
	src := aVideo(t, r)

	est, err := videoenhance.EstimateEnhance(ctx, r.Svc, upscale(src, ""))

	require.NoError(t, err)
	assert.False(t, est.Paid)
	assert.Empty(t, est.EstimateID)
	assert.Nil(t, est.Credits)
	assert.Contains(t, est.Message, "無料")
	assert.Equal(t, videoenhance.Output{Width: 960, Height: 1696, FPS: 16}, est.Output)
}

func TestEnhancersListsTheLocalOnesAndTheCloudWithTheKeyState(t *testing.T) {
	noKey := newRig(t, gentesttest.WithCloud(newCloud(false)))
	byID := func(list []modelcatalog.VideoEnhancer) map[string]modelcatalog.VideoEnhancer {
		out := map[string]modelcatalog.VideoEnhancer{}
		for _, e := range list {
			out[e.ID] = e
		}
		return out
	}

	list := byID(videoenhance.Enhancers(ctx, noKey.Svc))

	up := list[string(gentesttest.UpscaleVideoModel)]
	assert.True(t, up.Available)
	assert.False(t, up.Paid)
	assert.Equal(t, "upscale", up.Op)
	assert.Equal(t, "interpolate", list[string(gentesttest.InterpolateVideoModel)].Op)
	topaz := list[proteus]
	assert.False(t, topaz.Available, "no API key: not offered")
	assert.True(t, topaz.Paid)
	assert.Equal(t, "no_key", topaz.Unavailable)
	assert.Contains(t, topaz.UnavailableMessage, "API キー")
	assert.Equal(t, "Powered by Topaz Labs", topaz.Attribution)
	assert.Equal(t, "unknown", string(topaz.License.Commercial))
	assert.Equal(t, "https://developer.topazlabs.com/getting-started/faqs", topaz.License.SourceURL)
	assert.Equal(t, "2026-11-04", topaz.License.CheckedAt)

	withKey := newRig(t, gentesttest.WithCloud(newCloud(true)))
	assert.True(t, byID(videoenhance.Enhancers(ctx, withKey.Svc))[proteus].Available)
}

// enhanceCalls are the requests of the enhancement workflows the rig received.
func enhanceCalls(r *gentesttest.Rig) []gentesttest.Call {
	var out []gentesttest.Call
	for _, c := range r.Fake.Calls() {
		if c.Enhance != nil {
			out = append(out, c)
		}
	}
	return out
}
