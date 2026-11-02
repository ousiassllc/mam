package localcfg_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

const (
	int8Unet  = "ltx-2.5-22b-distilled-transformer-comfy-int8-convrot.safetensors"
	nvfp4Unet = "ltx-2.5-22b-distilled-transformer-nvfp4.safetensors"
	enhancer  = "gemma4_e2b_it_int8_convrot.safetensors"
)

func graphJSON(t *testing.T, id string) string {
	t.Helper()
	tpl, ok := localcfg.TemplateByID(id)
	require.True(t, ok, id)
	return string(tpl.Model.Workflow.Graph)
}

// FLF2V (Issue #968) is ComfyUI's single-stage video_ltx2_5_flf2v: the first and the last frame are two LTXVAddGuide nodes (frame 0 and
// frame -1, strength 0.7) on one empty latent, with no upscaler. The first_frame and last_frame assignments make the model take two images.
func TestLTX25FLF2VTemplatesPinTheFirstAndTheLastFrame(t *testing.T) {
	for _, id := range []string{"flf2v-ltx-2-5", "flf2v-ltx-2-5-enhanced", "flf2v-ltx-2-5-nvfp4", "flf2v-ltx-2-5-uncensored"} {
		tpl, _ := localcfg.TemplateByID(id)
		caps, ok := tpl.Model.VideoCaps()
		require.True(t, ok, id)
		assert.True(t, caps.FirstFrame, id)
		assert.True(t, caps.LastFrame, id)
		assert.True(t, caps.NativeAudio, id)
		assert.Equal(t, 8, tpl.Model.Workflow.FramesStep, id)

		g := graphOf(t, id)
		guides := idsOf(g, "LTXVAddGuide")
		require.Len(t, guides, 2, id)
		frames := map[string]string{}
		for _, gid := range guides {
			frames[string(g[gid].Inputs["frame_idx"])] = string(g[gid].Inputs["strength"])
		}
		assert.Equal(t, map[string]string{"0": "0.7", "-1": "0.7"}, frames, id+": the first and the last frame")
		assert.Empty(t, idsOf(g, "LatentUpscaleModelLoader"), id+": single stage, no upscaler")
		require.Len(t, idsOf(g, "LoadImage"), 2, id)
		require.Len(t, idsOf(g, "VAEDecodeTiled"), 1, id)
	}
}

// The variants are the base template with one thing changed, so a fix to the base is a fix to all of them.
func TestLTX25FLF2VVariantsDifferOnlyInWhatTheyAreFor(t *testing.T) {
	base := graphJSON(t, "flf2v-ltx-2-5")
	assert.Equal(t, strings.ReplaceAll(base, int8Unet, nvfp4Unet), graphJSON(t, "flf2v-ltx-2-5-nvfp4"))
	assert.Equal(t, strings.ReplaceAll(base, "gemma4-12b-with-proj-ltx-2.5-comfy-int8-convrot.safetensors", "gemma4-12b-uncensored-heretic-deepneuralnerd-ltx-2.5-int8-convrot.safetensors"),
		graphJSON(t, "flf2v-ltx-2-5-uncensored"))
}

// NVFP4 (Issue #968) swaps only the diffusion model file; the graph is the same as the INT8 one.
func TestLTX25NVFP4TemplatesDifferOnlyInTheDiffusionModel(t *testing.T) {
	for _, kind := range []string{"txt2video", "img2video"} {
		std, _ := localcfg.TemplateByID(kind + "-ltx-2-5")
		nv, ok := localcfg.TemplateByID(kind + "-ltx-2-5-nvfp4")
		require.True(t, ok, kind)
		assert.Equal(t, strings.ReplaceAll(string(std.Model.Workflow.Graph), int8Unet, nvfp4Unet), string(nv.Model.Workflow.Graph), kind)
		assert.Equal(t, std.Model.Workflow.Assignments, nv.Model.Workflow.Assignments, kind)
		assert.Contains(t, nv.Description, "Blackwell", kind)
	}
}

// The prompt enhancer (Issue #968) is ComfyUI's TextGenerateLTX2Prompt (core) with the Gemma 4 E2B text encoder: the prompt goes
// through it before the CLIPTextEncode. The enhanced templates are the plain ones plus that, and nothing else.
func TestLTX25EnhancedTemplatesRewriteThePromptWithTheEnhancer(t *testing.T) {
	for _, id := range []string{"txt2video-ltx-2-5-enhanced", "img2video-ltx-2-5-enhanced", "flf2v-ltx-2-5-enhanced"} {
		g := graphOf(t, id)
		tpl, _ := localcfg.TemplateByID(id)
		gen := idsOf(g, "TextGenerateLTX2Prompt")
		require.Len(t, gen, 1, id)

		clipID, clip := linked(t, g, gen[0], "clip")
		assert.Equal(t, "CLIPLoader", clip.ClassType, id)
		assert.JSONEq(t, `"`+enhancer+`"`, string(g[clipID].Inputs["clip_name"]), id)
		assert.JSONEq(t, `"ltxv"`, string(g[clipID].Inputs["type"]), id)

		var prompt, seed int
		for _, a := range tpl.Model.Workflow.Assignments {
			switch {
			case a.Param == localcfg.ParamPrompt:
				prompt++
				assert.Equal(t, gen[0], a.Node, id+": the user's prompt is what the enhancer expands")
				assert.Equal(t, "prompt", a.Input, id)
			case a.Param == localcfg.ParamSeed && a.Node == gen[0]:
				seed++
				assert.Equal(t, "sampling_mode.seed", a.Input, id)
			}
		}
		assert.Equal(t, 1, prompt, id)
		assert.Equal(t, 1, seed, id+": the enhancer is sampled with the seed of the request")

		var texts int
		for gid, n := range g {
			if n.ClassType != "CLIPTextEncode" {
				continue
			}
			var link []json.RawMessage
			if json.Unmarshal(n.Inputs["text"], &link) == nil {
				var from string
				require.NoError(t, json.Unmarshal(link[0], &from), gid)
				assert.Equal(t, gen[0], from, id+": the enhanced text reaches the positive prompt")
				texts++
			}
		}
		assert.Equal(t, 1, texts, id+": only the positive prompt is rewritten")
	}
	i2v := graphOf(t, "img2video-ltx-2-5-enhanced")
	_, img := linked(t, i2v, idsOf(i2v, "TextGenerateLTX2Prompt")[0], "image")
	assert.Equal(t, "LTXVPreprocess", img.ClassType, "the enhancer sees the first frame in I2V")

	for _, kind := range []string{"txt2video", "img2video"} {
		plain, enhanced := graphOf(t, kind+"-ltx-2-5"), graphOf(t, kind+"-ltx-2-5-enhanced")
		assert.Len(t, enhanced, len(plain)+2, kind+": the enhancer loader and the enhancer node are the only additions")
	}
}

// DFR (Issue #968) is ComfyUI's video_ltx2_5_dfr_i2v in three stages: half size with generated keyframes, x2 spatial with the detailing
// IC-LoRA, x2 temporal (48 fps). The first frame is the one image; the frames and the sizes are the request's.
func TestLTX25DFRTemplateIsTheThreeStageWorkflow(t *testing.T) {
	const id = "img2video-ltx-2-5-dfr"
	tpl, ok := localcfg.TemplateByID(id)
	require.True(t, ok)
	caps, ok := tpl.Model.VideoCaps()
	require.True(t, ok)
	assert.True(t, caps.FirstFrame)
	assert.False(t, caps.LastFrame)
	assert.True(t, caps.NativeAudio)
	assert.Equal(t, 24, tpl.Model.Workflow.FPS, "the frames are counted at 24 fps; the third stage doubles them")
	assert.Equal(t, 64, tpl.Model.Workflow.SizeMultiple)

	g := graphOf(t, id)
	assert.Len(t, idsOf(g, "SamplerCustomAdvanced"), 3, "three stages")
	assert.Len(t, idsOf(g, "LTXVLatentUpsampler"), 3, "a spatial upsample of the video, one of the keyframes, and the temporal one")
	assert.Len(t, idsOf(g, "LoraLoaderModelOnly"), 1, "the detailing IC-LoRA")
	assert.Len(t, idsOf(g, "LTXVAddGeneratedKeyframes"), 3)
	assert.Empty(t, idsOf(g, "TextGenerateLTX2Prompt"), "no prompt enhancement in DFR")
	var loaded []string
	for _, n := range g {
		if n.ClassType == "LatentUpscaleModelLoader" {
			loaded = append(loaded, string(n.Inputs["model_name"]))
		}
	}
	assert.ElementsMatch(t, []string{`"ltx-2.5-latent-spatial-upscaler-x2-bf16-1.0.safetensors"`, `"ltx-2.5-latent-temporal-upscaler-x2-bf16-1.0.safetensors"`}, loaded)
	lora := g[idsOf(g, "LoraLoaderModelOnly")[0]]
	assert.JSONEq(t, `"ltx-2.5-22b-ic-lora-pixel-spatial-upscaler-x2-1.0.safetensors"`, string(lora.Inputs["lora_name"]))
	assert.JSONEq(t, `"ltx-2.5-22b-distilled-transformer-comfy-int8-convrot.safetensors"`, string(g[idsOf(g, "UNETLoader")[0]].Inputs["unet_name"]))

	// every stage is seeded and every length is the request's
	seeds := 0
	for _, a := range tpl.Model.Workflow.Assignments {
		if a.Param == localcfg.ParamSeed {
			seeds++
			assert.Equal(t, "RandomNoise", g[a.Node].ClassType)
		}
	}
	assert.Equal(t, 3, seeds)
	assert.Equal(t, 2, tpl.Model.Workflow.Count(localcfg.ParamFrames), "the video latent and the audio latent")
}
