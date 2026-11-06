package gentestvideo_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/cloudenhance"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/files"
	mcpgentest "github.com/ousiassllc/mam/backend/internal/mcp/tools/gentest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/gentestvideo"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/jobs"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/venhance"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/videoenhance"
)

type rig struct {
	*mcptest.Env
	GT    *gentesttest.Rig
	Cloud *gentesttest.FakeCloud
}

// newRig is the MCP environment with the generation test, the video enhancement workflows, an ffprobe that reads A480p16 and a cloud (with or without its key).
func newRig(t *testing.T, key bool, opts ...gentesttest.Option) *rig {
	t.Helper()
	r := &rig{Cloud: &gentesttest.FakeCloud{
		IsConfigured: key, Result: []byte("TOPAZ"),
		ModelList: []cloudenhance.CloudModel{{ID: "prob-4", Provider: cloudenhance.ProviderTopaz, Op: cloudenhance.OpUpscale, Label: "Proteus"}},
		Quote:     cloudenhance.CloudEstimate{RequestID: "req-9", Credits: 7, Width: 960, Height: 1696, FPS: 16},
	}}
	opts = append([]gentesttest.Option{
		gentesttest.WithVideoEnhanceModels(), gentesttest.WithProbe(gentesttest.FakeProbe{Info: gentesttest.A480p16}), gentesttest.WithCloud(r.Cloud),
	}, opts...)
	r.Env = mcptest.New(t, func(env *mcptest.Env, reg *mcpkit.Registrar) {
		r.GT = gentesttest.NewOn(t, env.DB, opts...)
		env.Services.Gentest = r.GT.Svc
		jobs.Register(reg, env.Services)
		files.Register(reg, env.Services)
		mcpgentest.Register(reg, env.Services)
		gentestvideo.Register(reg, env.Services)
	})
	return r
}

func (r *rig) job(id string) mcpkit.JobStatus {
	r.T.Helper()
	return r.WaitJob(id, 20*time.Second, 10*time.Millisecond)
}

// aVideo runs a batch of one video and returns its result id.
func (r *rig) aVideo() string {
	r.T.Helper()
	ref, err := r.GT.Confirmed(gentest.Request{Media: "video", Models: []string{string(domain.ModelKling3)}, Prompts: []gentest.PromptInput{{Prompt: "a fox"}}, DurationSeconds: 5})
	require.NoError(r.T, err)
	return r.GT.Wait(ref.BatchID).Results[0].ID
}

func TestToolsAreRegisteredAndListTheModels(t *testing.T) {
	r := newRig(t, false)

	listed, err := r.Client.ListTools(t.Context(), nil)
	require.NoError(t, err)
	var names []string
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
	}
	assert.Subset(t, names, []string{"list_generation_video_enhancers", "estimate_generation_video_enhance", "enhance_generation_video"})

	var out gentestvideo.EnhancersOutput
	r.CallOK("list_generation_video_enhancers", map[string]any{}, &out)
	require.Len(t, out.Enhancers, 3)
	byID := map[string]int{}
	for i, e := range out.Enhancers {
		byID[e.ID] = i
	}
	up := out.Enhancers[byID[string(gentesttest.UpscaleVideoModel)]]
	assert.True(t, up.Available)
	assert.Equal(t, "local", up.Provider)
	topaz := out.Enhancers[byID["topaz:prob-4"]]
	assert.False(t, topaz.Available, "no key: not offered")
	assert.Contains(t, topaz.UnavailableMessage, "API キー")
	assert.Equal(t, "Powered by Topaz Labs", topaz.Attribution)
	assert.InDelta(t, 4, out.MaxScale, 0)
	assert.InDelta(t, 120, out.LocalMaxFPS, 0)
	assert.Equal(t, []int{2, 4}, out.Multipliers)
}

func TestALocalUpscaleRunsAtOnceAndTheResultRemembersTheSource(t *testing.T) {
	r := newRig(t, false)
	src := r.aVideo()

	var est videoenhance.Estimate
	r.CallOK("estimate_generation_video_enhance", map[string]any{"result_id": src, "op": "upscale", "scale": 2}, &est)
	assert.False(t, est.Paid)
	assert.Empty(t, est.EstimateID)
	assert.Contains(t, est.Message, "無料")

	var out gentestvideo.RunOutput
	r.CallOK("enhance_generation_video", map[string]any{"result_id": src, "op": "upscale", "scale": 2}, &out)
	r.job(out.JobID)

	require.NotEmpty(t, out.BatchID)
	var got record.ResultDetail
	r.CallOK("get_generation_result", map[string]any{"result_id": out.ResultID}, &got)
	assert.Equal(t, "succeeded", got.Status)
	require.NotNil(t, got.VideoEnhance)
	assert.Equal(t, src, got.VideoEnhance.SourceResultID)
	assert.Equal(t, "upscale", got.VideoEnhance.Op)
	assert.Equal(t, string(gentesttest.UpscaleVideoModel), got.VideoEnhance.Model)
	assert.InDelta(t, 2, got.VideoEnhance.Scale, 0)
}

func TestACloudModelNeedsTheKeyTheQuoteAndTheConfirmation(t *testing.T) {
	noKey := newRig(t, false)
	src := noKey.aVideo()
	msg := noKey.CallError("enhance_generation_video", map[string]any{"result_id": src, "op": "upscale", "scale": 2, "model": "topaz:prob-4"})
	assert.Contains(t, msg, "validation")
	assert.Contains(t, msg, "API キー")
	assert.Empty(t, noKey.Cloud.Runs)

	r := newRig(t, true)
	src = r.aVideo()
	args := map[string]any{"result_id": src, "op": "upscale", "scale": 2, "model": "topaz:prob-4"}
	var est videoenhance.Estimate
	r.CallOK("estimate_generation_video_enhance", args, &est)
	require.NotNil(t, est.Credits)
	assert.InDelta(t, 7, *est.Credits, 0)
	assert.Equal(t, "Powered by Topaz Labs", est.Attribution)
	assert.NotEmpty(t, est.EstimateID)

	run := func(extra map[string]any) string {
		a := map[string]any{}
		for k, v := range args {
			a[k] = v
		}
		for k, v := range extra {
			a[k] = v
		}
		return r.CallError("enhance_generation_video", a)
	}
	assert.Contains(t, run(nil), "estimate_generation_video_enhance", "without the quote")
	assert.True(t, strings.Contains(run(map[string]any{"estimate_id": est.EstimateID, "confirmed_credits": 1}), "conflict"), "wrong credits")
	assert.Contains(t, run(map[string]any{"estimate_id": "ve-nope", "confirmed_credits": 7}), "見積りの有効期限が切れました")
	assert.Empty(t, r.Cloud.Runs, "nothing was sent")

	var out gentestvideo.RunOutput
	a := map[string]any{"estimate_id": est.EstimateID, "confirmed_credits": *est.Credits}
	for k, v := range args {
		a[k] = v
	}
	r.CallOK("enhance_generation_video", a, &out)
	r.job(out.JobID)

	require.Len(t, r.Cloud.Runs, 1)
	var got record.ResultDetail
	r.CallOK("get_generation_result", map[string]any{"result_id": out.ResultID}, &got)
	assert.Equal(t, "succeeded", got.Status)
	require.NotNil(t, got.VideoEnhance)
	require.NotNil(t, got.VideoEnhance.Topaz)
	assert.Equal(t, "req-9", got.VideoEnhance.Topaz.RequestID)
	assert.Equal(t, "Powered by Topaz Labs", got.VideoEnhance.Topaz.Attribution)
}

func TestAnImageResultAndAnUnsuitableTargetAreRefused(t *testing.T) {
	r := newRig(t, false)
	ref, err := r.GT.Confirmed(gentest.Request{Media: "image", Models: []string{string(domain.ModelFlux2)}, Prompts: []gentest.PromptInput{{Prompt: "a fox"}}})
	require.NoError(t, err)
	image := r.GT.Wait(ref.BatchID).Results[0].ID
	src := r.aVideo()

	assert.Contains(t, r.CallError("enhance_generation_video", map[string]any{"result_id": image, "op": "upscale", "scale": 2}), "成功した動画ではありません")
	msg := r.CallError("enhance_generation_video", map[string]any{
		"result_id": src, "op": "interpolate", "target_fps": 24, "model": string(gentesttest.InterpolateVideoModel),
	})
	assert.Contains(t, msg, "整数倍", "24 fps is not a multiple of 16 fps")
	assert.Contains(t, r.CallError("estimate_generation_video_enhance", map[string]any{"result_id": src, "op": "upscale", "scale": 9}), "倍率")
}

func TestWithoutFfprobeTheToolSaysHowToFixIt(t *testing.T) {
	r := newRig(t, false, gentesttest.WithProbe(nil))
	src := r.aVideo()

	msg := r.CallError("estimate_generation_video_enhance", map[string]any{"result_id": src, "op": "upscale", "scale": 2})

	assert.Contains(t, msg, `"code":"ffmpeg_missing"`)
	assert.Contains(t, msg, "ffmpeg")
	assert.Contains(t, msg, "付随ソフト", "the remedy names where ffmpeg is set up: the model manager")
	assert.Contains(t, msg, "/model-manager/companion#mm-ffmpeg")
}

// An adult result may go to Topaz (Issue #1031, decision of 2026-11-05): the estimate carries the warning, and without adult_confirmed nothing is sent.
func TestAnAdultResultNeedsTheWarningConfirmedOverMCP(t *testing.T) {
	r := newRig(t, true, gentesttest.WithCatalogInfo(func(id string) (modelfilter.CatalogInfo, bool) {
		return modelfilter.CatalogInfo{Adult: id == string(domain.ModelKling3)}, true
	}))
	src := r.aVideo()
	args := map[string]any{"result_id": src, "op": "upscale", "scale": 2, "model": "topaz:prob-4"}

	var est videoenhance.Estimate
	r.CallOK("estimate_generation_video_enhance", args, &est)
	require.NotNil(t, est.Credits)
	assert.True(t, est.Adult)
	assert.Equal(t, venhance.AdultWarning, est.AdultWarning)

	run := map[string]any{"estimate_id": est.EstimateID, "confirmed_credits": *est.Credits}
	for k, v := range args {
		run[k] = v
	}
	msg := r.CallError("enhance_generation_video", run)
	assert.Contains(t, msg, "validation")
	assert.Contains(t, msg, "Topaz Labs", "the same warning is returned")
	assert.Empty(t, r.Cloud.Runs, "nothing is sent without adult_confirmed")

	run["adult_confirmed"] = true
	var out gentestvideo.RunOutput
	r.CallOK("enhance_generation_video", run, &out)
	r.job(out.JobID)
	require.Len(t, r.Cloud.Runs, 1)
}

// A result that is not adult has no warning and needs no adult_confirmed.
func TestAResultThatIsNotAdultHasNoWarningOverMCP(t *testing.T) {
	r := newRig(t, true)
	src := r.aVideo()
	args := map[string]any{"result_id": src, "op": "upscale", "scale": 2, "model": "topaz:prob-4"}

	var est videoenhance.Estimate
	r.CallOK("estimate_generation_video_enhance", args, &est)
	require.NotNil(t, est.Credits)
	assert.False(t, est.Adult)
	assert.Empty(t, est.AdultWarning)

	args["estimate_id"], args["confirmed_credits"] = est.EstimateID, *est.Credits
	var out gentestvideo.RunOutput
	r.CallOK("enhance_generation_video", args, &out)
	r.job(out.JobID)
	require.Len(t, r.Cloud.Runs, 1)
}
