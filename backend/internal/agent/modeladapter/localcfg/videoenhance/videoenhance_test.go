package videoenhance_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/videoenhance"
)

func template(t *testing.T, id string) localcfg.Model {
	t.Helper()
	tpl, ok := localcfg.TemplateByID(id)
	require.True(t, ok, id)
	return tpl.Model
}

// withWorkflow is the model with a changed copy of its workflow.
func withWorkflow(m localcfg.Model, f func(*localcfg.Workflow)) localcfg.Model {
	w := *m.Workflow
	w.Assignments = append([]localcfg.Assignment{}, w.Assignments...)
	f(&w)
	m.Workflow = &w
	return m
}

func TestTheVideoEnhanceTemplatesAreValidWorkflowsThatTakeTheSourceVideo(t *testing.T) {
	want := map[string]videoenhance.Op{
		"video-upscale-seedvr2-3b":     videoenhance.Upscale,
		"video-interpolate-rife-v4.25": videoenhance.Interpolate,
		"video-interpolate-rife-v4.26": videoenhance.Interpolate,
	}
	for id, op := range want {
		m := template(t, id)
		assert.True(t, m.IsVideoEnhance(), id)
		assert.Equal(t, op, m.Workflow.VideoEnhance.Op, id)
		assert.Equal(t, localcfg.MediaVideo, m.Media, id)
		assert.Nil(t, m.Video, "the source video has the length and the size: %s", id)
		assert.False(t, m.Workflow.Has(localcfg.ParamPrompt), id)
		assert.Equal(t, 1, m.Workflow.Count(localcfg.ParamSourceVideo), id)
		assert.Empty(t, localcfg.ValidateSaved(m), id)
		_, hasCaps := m.VideoCaps()
		assert.False(t, hasCaps, "an enhancement workflow is no model to generate with: %s", id)
	}
	up := template(t, "video-upscale-seedvr2-3b").Workflow
	assert.Equal(t, 1, up.Count(localcfg.ParamVideoScale))
	assert.Equal(t, 2073600, up.VideoEnhance.MaxOutputPixels)
	for _, id := range []string{"video-interpolate-rife-v4.25", "video-interpolate-rife-v4.26"} {
		w := template(t, id).Workflow
		assert.Equal(t, 1, w.Count(localcfg.ParamFrameMultiplier), id)
		assert.Equal(t, 1, w.Count(localcfg.ParamOutputFPS), id)
	}
}

// A 16 GB card must not fall over: fp8 weights, a tiled VAE and the temporal chunking of SeedVR2 (auto), and the audio of the source goes
// back into the video.
func TestSeedVR2IsWiredForSixteenGigabytesAndKeepsTheAudio(t *testing.T) {
	g, err := localcfg.ParseGraph(template(t, "video-upscale-seedvr2-3b").Workflow.Graph)
	require.NoError(t, err)
	classes := map[string]localcfg.Node{}
	for _, n := range g {
		classes[n.ClassType] = n
	}
	for _, c := range []string{"VAEEncodeTiled", "VAEDecodeTiled", "SeedVR2TemporalChunk", "SeedVR2TemporalMerge", "SeedVR2Preprocess", "SeedVR2PostProcessing", "GetVideoComponents"} {
		assert.Contains(t, classes, c)
	}
	assert.JSONEq(t, `"seedvr2_3b_fp8_e4m3fn.safetensors"`, string(classes["UNETLoader"].Inputs["unet_name"]))
	assert.JSONEq(t, `"auto"`, string(classes["SeedVR2TemporalChunk"].Inputs["chunking_mode"]))
	assert.JSONEq(t, `512`, string(classes["VAEEncodeTiled"].Inputs["tile_size"]))
	assert.JSONEq(t, `512`, string(classes["VAEDecodeTiled"].Inputs["tile_size"]))
	assert.JSONEq(t, `1`, string(classes["KSampler"].Inputs["steps"]), "SeedVR2 is a one-step model")
	assert.JSONEq(t, `["2",1]`, string(classes["CreateVideo"].Inputs["audio"]), "the audio of the source goes back into the video")
	assert.JSONEq(t, `["2",2]`, string(classes["CreateVideo"].Inputs["fps"]), "the frame rate of the source is kept")
}

func TestRIFEKeepsTheAudioAndTakesTheFramesOfTheSource(t *testing.T) {
	for id, file := range map[string]string{"video-interpolate-rife-v4.25": "rife_v4.25.safetensors", "video-interpolate-rife-v4.26": "rife_v4.26.safetensors"} {
		g, err := localcfg.ParseGraph(template(t, id).Workflow.Graph)
		require.NoError(t, err)
		classes := map[string]localcfg.Node{}
		for _, n := range g {
			classes[n.ClassType] = n
		}
		assert.JSONEq(t, `"`+file+`"`, string(classes["FrameInterpolationModelLoader"].Inputs["model_name"]), id)
		assert.JSONEq(t, `["2",0]`, string(classes["FrameInterpolate"].Inputs["images"]), id)
		assert.JSONEq(t, `["2",1]`, string(classes["CreateVideo"].Inputs["audio"]), id)
	}
}

func TestAnEnhancementWorkflowIsLeftOutOfTheModelLists(t *testing.T) {
	endpoints := map[string]localcfg.Endpoint{"box": {Kind: localcfg.KindComfyUI, BaseURL: "http://127.0.0.1:8188"}}
	models := map[string]localcfg.Model{}
	for _, id := range []string{"video-upscale-seedvr2-3b", "video-interpolate-rife-v4.25", "img2video-wan22-5b"} {
		models["box/"+id] = template(t, id)
	}
	assert.Equal(t, []string{"comfyui:box/img2video-wan22-5b"}, localcfg.IDs(endpoints, models, localcfg.MediaVideo))
	for key, m := range models {
		assert.Empty(t, localcfg.ValidateModel(key, m, endpoints), key)
	}
}

func TestValidateRefusesWhatCannotRun(t *testing.T) {
	up := template(t, "video-upscale-seedvr2-3b")
	rife := template(t, "video-interpolate-rife-v4.25")
	without := func(p localcfg.Param) func(*localcfg.Workflow) {
		return func(w *localcfg.Workflow) {
			var kept []localcfg.Assignment
			for _, a := range w.Assignments {
				if a.Param != p {
					kept = append(kept, a)
				}
			}
			w.Assignments = kept
		}
	}
	cases := map[string]struct {
		model localcfg.Model
		want  string
	}{
		"no source":                 {withWorkflow(up, without(localcfg.ParamSourceVideo)), "exactly one source_video"},
		"no scale":                  {withWorkflow(up, without(localcfg.ParamVideoScale)), "exactly one video_scale"},
		"scale in rife":             {withWorkflow(rife, func(w *localcfg.Workflow) { w.Assignments = append(w.Assignments, up.Workflow.Assignments[1]) }), "no video_scale"},
		"no multiplier":             {withWorkflow(rife, without(localcfg.ParamFrameMultiplier)), "frame_multiplier"},
		"no output fps":             {withWorkflow(rife, without(localcfg.ParamOutputFPS)), "output_fps"},
		"fps in upscale":            {withWorkflow(up, func(w *localcfg.Workflow) { w.Assignments = append(w.Assignments, rife.Workflow.Assignments[2]) }), "neither frame_multiplier nor output_fps"},
		"no label":                  {withWorkflow(up, func(w *localcfg.Workflow) { s := *w.VideoEnhance; s.Label = ""; w.VideoEnhance = &s }), "label is empty"},
		"unknown op":                {withWorkflow(up, func(w *localcfg.Workflow) { s := *w.VideoEnhance; s.Op = "x"; w.VideoEnhance = &s }), "op must be"},
		"negative pixels":           {withWorkflow(up, func(w *localcfg.Workflow) { s := *w.VideoEnhance; s.MaxOutputPixels = -1; w.VideoEnhance = &s }), "max_output_pixels"},
		"an image one":              {func() localcfg.Model { m := up; m.Media = localcfg.MediaImage; return m }(), "only a video workflow"},
		"unmarked takes the values": {withWorkflow(up, func(w *localcfg.Workflow) { w.VideoEnhance = nil }), "belong to a video enhancement workflow"},
	}
	for name, c := range cases {
		errs := localcfg.ValidateSaved(c.model)
		require.NotEmpty(t, errs, name)
		var text string
		for _, e := range errs {
			text += e.Error() + "\n"
		}
		assert.Contains(t, text, c.want, name)
	}
}
