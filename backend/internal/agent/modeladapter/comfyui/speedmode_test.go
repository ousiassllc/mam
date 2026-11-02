package comfyui_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyuitest"
)

func lightx2vRun(t *testing.T, mode string) map[string]struct {
	ClassType string                     `json:"class_type"`
	Inputs    map[string]json.RawMessage `json:"inputs"`
} {
	t.Helper()
	f := comfyuitest.New(t)
	f.OutputFile, f.OutputNode, f.OutputBody = "clip.mp4", "59", []byte("mp4")
	b := backend(t, f, "img2video-wan22-14b-lightx2v", nil)
	_, err := b.GenerateVideo(ctx(t), "", modeladapter.VideoRequest{
		Prompt: "a dog runs", DurationSeconds: 5, Resolution: "480p", AspectRatio: "16:9", SpeedMode: mode,
		FirstFrame: &modeladapter.Input{Data: comfyuitest.PNG, MimeType: "image/png"},
	})
	require.NoError(t, err)
	return f.Graph()
}

// Wan 2.2 14B with the LightX2V LoRA runs the 4-step values of ComfyUI's official template by default (and when "fast" is asked): the
// LoRA of the high noise and the low noise model each sit between their UNETLoader and ModelSamplingSD3, 4 steps at cfg 1, handed over
// from the high to the low noise model after step 2.
func TestLightX2VRunsTheFourStepValuesByDefault(t *testing.T) {
	for _, mode := range []string{"", "fast"} {
		t.Run("mode "+mode, func(t *testing.T) {
			g := lightx2vRun(t, mode)
			assert.Equal(t, "a dog runs", str(g["6"].Inputs["text"]))
			assert.Equal(t, "wan2.2_i2v_A14b_high_noise_lora_rank64_lightx2v_4step_1022.safetensors", str(g["97"].Inputs["lora_name"]))
			assert.Equal(t, "wan2.2_i2v_A14b_low_noise_lora_rank64_lightx2v_4step_1022.safetensors", str(g["98"].Inputs["lora_name"]))
			assert.JSONEq(t, `["97",0]`, string(g["54"].Inputs["model"]), "the high noise LoRA feeds the high noise sampler")
			assert.JSONEq(t, `["98",0]`, string(g["55"].Inputs["model"]), "the low noise LoRA feeds the low noise sampler")
			for _, id := range []string{"3", "4"} {
				assert.Equal(t, 4.0, num(g[id].Inputs["steps"]), id)
				assert.Equal(t, 1.0, num(g[id].Inputs["cfg"]), id)
			}
			assert.Equal(t, 2.0, num(g["3"].Inputs["end_at_step"]))
			assert.Equal(t, 2.0, num(g["4"].Inputs["start_at_step"]))
			assert.Equal(t, 5.0, num(g["54"].Inputs["shift"]))
		})
	}
}

// "standard" is the ordinary Wan 2.2 14B again: the LoRA is left out (its nodes are not even sent, so the server does not load files the
// run does not use), the models feed the samplers directly, 20 steps at cfg 3.5 with the hand-over after step 10.
func TestTheStandardSpeedModeLeavesTheLoRAOutAndRestoresTheUsualValues(t *testing.T) {
	g := lightx2vRun(t, "standard")
	assert.NotContains(t, g, "97")
	assert.NotContains(t, g, "98")
	assert.JSONEq(t, `["95",0]`, string(g["54"].Inputs["model"]))
	assert.JSONEq(t, `["96",0]`, string(g["55"].Inputs["model"]))
	for _, id := range []string{"3", "4"} {
		assert.Equal(t, 20.0, num(g[id].Inputs["steps"]), id)
		assert.Equal(t, 3.5, num(g[id].Inputs["cfg"]), id)
	}
	assert.Equal(t, 10.0, num(g["3"].Inputs["end_at_step"]))
	assert.Equal(t, 10.0, num(g["4"].Inputs["start_at_step"]))
	assert.Equal(t, 8.0, num(g["54"].Inputs["shift"]))
	assert.Equal(t, 8.0, num(g["55"].Inputs["shift"]))
	for _, id := range []string{"95", "96", "38", "39", "57", "56", "58", "59"} {
		assert.Contains(t, g, id, "the rest of the graph is untouched")
	}
}

// A mode the workflow does not have is refused (the generation test warns first and sends none); a workflow without modes ignores one.
func TestAnUnknownSpeedModeIsRefusedAndAWorkflowWithoutModesIgnoresIt(t *testing.T) {
	f := comfyuitest.New(t)
	f.OutputFile, f.OutputNode, f.OutputBody = "clip.mp4", "59", []byte("mp4")
	b := backend(t, f, "img2video-wan22-14b-lightx2v", nil)
	_, err := b.GenerateVideo(ctx(t), "", modeladapter.VideoRequest{
		Prompt: "x", DurationSeconds: 5, Resolution: "480p", AspectRatio: "16:9", SpeedMode: "turbo",
		FirstFrame: &modeladapter.Input{Data: comfyuitest.PNG, MimeType: "image/png"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `no speed mode "turbo"`)
	assert.Zero(t, f.Count("POST /prompt"), "nothing was submitted")

	plain := comfyuitest.New(t)
	plain.OutputFile, plain.OutputNode, plain.OutputBody = "clip.mp4", "58", []byte("mp4")
	_, err = backend(t, plain, "txt2video-wan22-5b", nil).GenerateVideo(ctx(t), "", modeladapter.VideoRequest{
		Prompt: "x", DurationSeconds: 3, Resolution: "480p", AspectRatio: "16:9", SpeedMode: "fast",
	})
	require.NoError(t, err)
}

// The Remix T2V workflow is made of core nodes only, from the author's own ComfyUI workflow: EmptyHunyuanLatentVideo for the latent, 12
// steps at cfg 1, the hand-over after step 10, and the prompt, the seed and the size reach their nodes.
func TestRemixTextToVideoRunsWithCoreNodes(t *testing.T) {
	f := comfyuitest.New(t)
	f.OutputFile, f.OutputNode, f.OutputBody = "clip.mp4", "60", []byte("mp4")
	seed := int64(7)
	_, err := backend(t, f, "txt2video-wan22-14b-remix", nil).GenerateVideo(ctx(t), "", modeladapter.VideoRequest{
		Prompt: "a dancer", NegativePrompt: "blur", DurationSeconds: 5, Resolution: "480p", AspectRatio: "16:9", Seed: &seed,
	})
	require.NoError(t, err)
	g := f.Graph()
	assert.Equal(t, "a dancer", str(g["6"].Inputs["text"]))
	assert.Equal(t, "blur", str(g["7"].Inputs["text"]))
	assert.Equal(t, 7.0, num(g["3"].Inputs["noise_seed"]))
	assert.Equal(t, 81.0, num(g["59"].Inputs["length"]))
	assert.Equal(t, 848.0, num(g["59"].Inputs["width"]), "16:9 at 480p, rounded to a multiple of 16")
	assert.Equal(t, 480.0, num(g["59"].Inputs["height"]))
	assert.Equal(t, 12.0, num(g["3"].Inputs["steps"]))
	assert.Equal(t, 1.0, num(g["3"].Inputs["cfg"]))
	for id, n := range g {
		assert.NotContains(t, n.ClassType, "KJ", id+": no custom node")
		assert.NotContains(t, n.ClassType, "WanVideo", id+": no custom node")
	}
}
