package templatestest

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

func inputOf(g localcfg.Graph, id, input string) string { return string(g[id].Inputs[input]) }

// NSFW Wan 1.3B (Issue #1012) is built as ComfyUI's own Wan 2.1 T2V 1.3B example (comfyanonymous/ComfyUI_examples, wan/text_to_video_wan.json):
// UNETLoader, a CLIPLoader of type wan, the Wan 2.1 VAE, ModelSamplingSD3 with shift 8, an EmptyHunyuanLatentVideo, 30 steps, uni_pc/simple and cfg 6.
func TestWan21T2VTemplateFollowsTheOfficialExample(t *testing.T) {
	g := graphOf(t, "txt2video-wan21-1b3-nsfw")
	assert.Equal(t, `"nsfw_wan_1.3b_exp_e14.safetensors"`, inputOf(g, "37", "unet_name"))
	assert.Equal(t, `"wan"`, inputOf(g, "38", "type"))
	assert.Equal(t, `"umt5_xxl_fp8_e4m3fn_scaled.safetensors"`, inputOf(g, "38", "clip_name"))
	assert.Equal(t, `"wan_2.1_vae.safetensors"`, inputOf(g, "39", "vae_name"))
	assert.Equal(t, "8", inputOf(g, "48", "shift"))
	from, n := linked(t, g, "3", "model")
	assert.Equal(t, "48", from)
	assert.Equal(t, "ModelSamplingSD3", n.ClassType)
	from, n = linked(t, g, "48", "model")
	assert.Equal(t, "37", from)
	assert.Equal(t, "UNETLoader", n.ClassType)
	from, n = linked(t, g, "3", "latent_image")
	assert.Equal(t, "EmptyHunyuanLatentVideo", n.ClassType, from)
	assert.Equal(t, "30", inputOf(g, "3", "steps"))
	assert.Equal(t, "6", inputOf(g, "3", "cfg"))
	assert.Equal(t, `"uni_pc"`, inputOf(g, "3", "sampler_name"))
	assert.Equal(t, `"simple"`, inputOf(g, "3", "scheduler"))
	for _, enc := range []string{"6", "7"} {
		from, _ = linked(t, g, enc, "clip")
		assert.Equal(t, "38", from)
	}

	tpl, _ := localcfg.TemplateByID("txt2video-wan21-1b3-nsfw")
	caps, ok := tpl.Model.VideoCaps()
	require.True(t, ok)
	assert.False(t, caps.FirstFrame, "text to video only")
	assert.Equal(t, 16, tpl.Model.Workflow.FPS)
	assert.Equal(t, []string{"480p"}, tpl.Model.Video.Resolutions, "the 1.3B model is for 480p")
}

// graphWithout is the graph with the nodes removed (to compare what two templates share).
func graphWithout(t *testing.T, id string, drop ...string) map[string]any {
	t.Helper()
	tpl, _ := localcfg.TemplateByID(id)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(tpl.Model.Workflow.Graph, &raw))
	for _, d := range drop {
		delete(raw, d)
	}
	return raw
}

// The NSFW-Anime-wan-5B templates are the Wan 2.2 5B ones with the diffusion model replaced and the author's 40 steps: nothing else differs.
func TestWan5BAnimeTemplatesDifferOnlyInTheDiffusionModelAndTheSteps(t *testing.T) {
	for _, kind := range []string{"txt2video", "img2video"} {
		base, _ := localcfg.TemplateByID(kind + "-wan22-5b")
		anime, ok := localcfg.TemplateByID(kind + "-wan22-5b-nsfw-anime")
		require.True(t, ok, kind)
		assert.Equal(t, graphWithout(t, kind+"-wan22-5b", "37", "3"), graphWithout(t, kind+"-wan22-5b-nsfw-anime", "37", "3"), kind)
		g := graphOf(t, kind+"-wan22-5b-nsfw-anime")
		assert.Equal(t, `"nsfw_anime_wan_5b_3.0pro.safetensors"`, inputOf(g, "37", "unet_name"), kind)
		assert.Equal(t, "40", inputOf(g, "3", "steps"), kind)
		assert.Equal(t, "5", inputOf(g, "3", "cfg"), kind)
		assert.Equal(t, `"uni_pc"`, inputOf(g, "3", "sampler_name"), kind)
		assert.Equal(t, base.Model.Workflow.Assignments, anime.Model.Workflow.Assignments, kind)
		assert.Equal(t, 24, anime.Model.Workflow.FPS, kind)
	}
}

// The 5B LoRA templates are the fp8 5B ones with a LoraLoaderModelOnly (strength 1) between the loader and ModelSamplingSD3.
func TestWan5BLoRATemplatesPutTheLoRAAfterTheLoader(t *testing.T) {
	for _, kind := range []string{"txt2video", "img2video"} {
		id := kind + "-wan22-5b-lora"
		base, _ := localcfg.TemplateByID(kind + "-wan22-5b-fp8")
		lora, ok := localcfg.TemplateByID(id)
		require.True(t, ok, id)
		assert.Equal(t, graphWithout(t, kind+"-wan22-5b-fp8", "48"), graphWithout(t, id, "48", "60"), id)
		assert.Equal(t, base.Model.Workflow.Assignments, lora.Model.Workflow.Assignments, id)

		g := graphOf(t, id)
		require.Equal(t, []string{"60"}, idsOf(g, "LoraLoaderModelOnly"), id)
		assert.Equal(t, `"c0wg1rl_reverse_c0wg1rl_wan22_5b_ti2v_e380.safetensors"`, inputOf(g, "60", "lora_name"), id)
		assert.JSONEq(t, "1", inputOf(g, "60", "strength_model"), id)
		from, n := linked(t, g, "60", "model")
		assert.Equal(t, "37", from, id)
		assert.Equal(t, "UNETLoader", n.ClassType, id)
		from, n = linked(t, g, "48", "model")
		assert.Equal(t, "60", from, id)
		assert.Equal(t, "LoraLoaderModelOnly", n.ClassType, id)
	}
	caps := func(id string) bool {
		tpl, _ := localcfg.TemplateByID(id)
		c, ok := tpl.Model.VideoCaps()
		require.True(t, ok, id)
		return c.FirstFrame
	}
	assert.False(t, caps("txt2video-wan22-5b-lora"))
	assert.True(t, caps("img2video-wan22-5b-lora"))
}

// rzgar's R1 v2 is the Wan 2.2 14B first-frame template with the two merged experts, the author's sampler (dpmpp_2m_sde, sgm_uniform, cfg 1) and the
// 2 + 3 split of five steps (the handover at 0.4 of the steps).
func TestWan14BR1TemplateIsTheBaseWithTheMergedExpertsAndTheAuthorsSampler(t *testing.T) {
	const id = "img2video-wan22-14b-rzgar-r1"
	g := graphOf(t, id)
	assert.Equal(t, `"Wan2.2_I2V_High_R1_n54w_v2.safetensors"`, inputOf(g, "95", "unet_name"))
	assert.Equal(t, `"Wan2.2_I2V_Low_R1_n54w_v2.safetensors"`, inputOf(g, "96", "unet_name"))
	for _, n := range []string{"3", "4"} {
		assert.Equal(t, "5", inputOf(g, n, "steps"))
		assert.Equal(t, "1", inputOf(g, n, "cfg"))
		assert.Equal(t, `"dpmpp_2m_sde"`, inputOf(g, n, "sampler_name"))
		assert.Equal(t, `"sgm_uniform"`, inputOf(g, n, "scheduler"))
	}
	assert.Equal(t, "2", inputOf(g, "3", "end_at_step"))
	assert.Equal(t, "2", inputOf(g, "4", "start_at_step"), "the low noise expert takes over where the high noise one stops")
	assert.Equal(t, graphWithout(t, "img2video-wan22-14b", "95", "96", "3", "4"), graphWithout(t, id, "95", "96", "3", "4"))
	tpl, _ := localcfg.TemplateByID(id)
	base, _ := localcfg.TemplateByID("img2video-wan22-14b")
	assert.Equal(t, 16, tpl.Model.Workflow.FPS)
	caps, ok := tpl.Model.VideoCaps()
	require.True(t, ok)
	assert.True(t, caps.FirstFrame)
	handover := 0
	for i, a := range tpl.Model.Workflow.Assignments {
		if a.Ratio != 0 {
			handover++
			assert.InDelta(t, 0.4, a.Ratio, 1e-9, "2 of 5 steps are the high noise expert's")
			b := base.Model.Workflow.Assignments[i]
			assert.Equal(t, b.Node, a.Node)
			assert.Equal(t, b.Input, a.Input)
		}
	}
	assert.Equal(t, 2, handover)
}

// The Mega merge is one checkpoint (model, text encoder and VAE inside) run as VACE with the control strength 0, which is a text to video: the loader
// feeds the encoders, the sampler's model and both decoders; the conditioning and the latent come from WanVaceToVideo, whose trim_latent output
// goes to TrimVideoLatent (the output of the sampler is trimmed before the decode), and the author's sampler is dpmpp_sde/beta with 4 steps and cfg 1.
func TestWan14BRapidMegaTemplateRunsVACEWithoutAControlAsTextToVideo(t *testing.T) {
	const id = "txt2video-wan22-14b-rapid-mega"
	g := graphOf(t, id)
	assert.Equal(t, []string{"26"}, idsOf(g, "CheckpointLoaderSimple"))
	for _, class := range []string{"UNETLoader", "CLIPLoader", "VAELoader", "LoadImage"} {
		assert.Empty(t, idsOf(g, class), class+": everything is in the checkpoint, and no picture is taken")
	}
	for _, enc := range []string{"6", "7"} {
		from, _ := linked(t, g, enc, "clip")
		assert.Equal(t, "26", from)
	}
	from, _ := linked(t, g, "28", "vae")
	assert.Equal(t, "26", from)
	from, _ = linked(t, g, "8", "vae")
	assert.Equal(t, "26", from)
	assert.Equal(t, "0", inputOf(g, "28", "strength"), "no control: a text to video")
	for _, in := range []string{"control_video", "control_masks", "reference_image"} {
		assert.NotContains(t, g["28"].Inputs, in)
	}
	from, n := linked(t, g, "32", "model")
	assert.Equal(t, "26", from)
	assert.Equal(t, "CheckpointLoaderSimple", n.ClassType)
	assert.Equal(t, "8", inputOf(g, "32", "shift"))
	from, _ = linked(t, g, "3", "model")
	assert.Equal(t, "32", from)
	from, _ = linked(t, g, "3", "latent_image")
	assert.Equal(t, "28", from)
	assert.JSONEq(t, `["28",3]`, inputOf(g, "29", "trim_amount"), "the trim_latent output of WanVaceToVideo")
	from, _ = linked(t, g, "29", "samples")
	assert.Equal(t, "3", from)
	from, _ = linked(t, g, "8", "samples")
	assert.Equal(t, "29", from)
	assert.Equal(t, "4", inputOf(g, "3", "steps"))
	assert.Equal(t, "1", inputOf(g, "3", "cfg"))
	assert.Equal(t, `"dpmpp_sde"`, inputOf(g, "3", "sampler_name"))
	assert.Equal(t, `"beta"`, inputOf(g, "3", "scheduler"))
	tpl, _ := localcfg.TemplateByID(id)
	caps, _ := tpl.Model.VideoCaps()
	assert.False(t, caps.FirstFrame, "text to video only")
}

// The Mystic XXX template is the official Wan 2.2 14B T2V workflow (20 steps, euler/simple, cfg 3.5, the handover at the tenth step, shift 8) with a
// LoRA on each expert, between its loader and its ModelSamplingSD3; the high noise LoRA goes to the high noise expert.
func TestWan14BT2VLoRATemplateIsTheOfficialWorkflowWithALoRAOnEachExpert(t *testing.T) {
	const id = "txt2video-wan22-14b-lora"
	g := graphOf(t, id)
	assert.Equal(t, `"wan2.2_t2v_high_noise_14B_fp8_scaled.safetensors"`, inputOf(g, "95", "unet_name"))
	assert.Equal(t, `"wan2.2_t2v_low_noise_14B_fp8_scaled.safetensors"`, inputOf(g, "96", "unet_name"))
	require.Equal(t, []string{"97", "98"}, idsOf(g, "LoraLoaderModelOnly"))
	for lora, want := range map[string]struct{ file, unet, sampling string }{
		"97": {"Wan2.2-High-MysticXXX.safetensors", "95", "54"},
		"98": {"Wan2.2-Low-MysticXXX.safetensors", "96", "55"},
	} {
		assert.JSONEq(t, `"`+want.file+`"`, inputOf(g, lora, "lora_name"))
		assert.JSONEq(t, "1", inputOf(g, lora, "strength_model"))
		from, _ := linked(t, g, lora, "model")
		assert.Equal(t, want.unet, from)
		from, _ = linked(t, g, want.sampling, "model")
		assert.Equal(t, lora, from)
		assert.Equal(t, "8", inputOf(g, want.sampling, "shift"))
	}
	for _, n := range []string{"3", "4"} {
		assert.Equal(t, "20", inputOf(g, n, "steps"))
		assert.Equal(t, "3.5", inputOf(g, n, "cfg"))
		assert.Equal(t, `"euler"`, inputOf(g, n, "sampler_name"))
	}
	assert.Equal(t, "10", inputOf(g, "3", "end_at_step"))
	assert.Equal(t, "10", inputOf(g, "4", "start_at_step"))
	assert.Equal(t, `"enable"`, inputOf(g, "3", "return_with_leftover_noise"))
	assert.Equal(t, `"disable"`, inputOf(g, "4", "add_noise"))
	tpl, _ := localcfg.TemplateByID(id)
	caps, _ := tpl.Model.VideoCaps()
	assert.False(t, caps.FirstFrame, "text to video")
	for _, a := range tpl.Model.Workflow.Assignments {
		if a.Ratio != 0 {
			assert.InDelta(t, 0.5, a.Ratio, 1e-9)
		}
	}
}

// HunyuanVideo with a LoRA is ComfyUI's official T2V example (the dual CLIP loader of type hunyuan_video, FluxGuidance 6 into a BasicGuider, the
// shift 7 on the guider's model, the scheduler on the loader, SamplerCustomAdvanced, tiled decode) with the LoRA after the loader.
func TestHunyuanVideoLoRATemplateFollowsTheOfficialExample(t *testing.T) {
	const id = "txt2video-hunyuan-video-lora"
	g := graphOf(t, id)
	assert.Equal(t, `"hunyuan_video"`, inputOf(g, "11", "type"))
	assert.Equal(t, `"clip_l.safetensors"`, inputOf(g, "11", "clip_name1"))
	assert.Equal(t, `"llava_llama3_fp8_scaled.safetensors"`, inputOf(g, "11", "clip_name2"))
	assert.Equal(t, `"fp8_e4m3fn"`, inputOf(g, "12", "weight_dtype"))
	from, n := linked(t, g, "20", "model")
	assert.Equal(t, "12", from)
	assert.Equal(t, "UNETLoader", n.ClassType)
	assert.JSONEq(t, "1", inputOf(g, "20", "strength_model"))
	assert.Equal(t, `"hunyuan_video_nsfw_e7.safetensors"`, inputOf(g, "20", "lora_name"))
	from, _ = linked(t, g, "67", "model")
	assert.Equal(t, "20", from, "the guider's model carries the LoRA")
	assert.Equal(t, "7", inputOf(g, "67", "shift"))
	from, _ = linked(t, g, "22", "model")
	assert.Equal(t, "67", from)
	from, n = linked(t, g, "22", "conditioning")
	assert.Equal(t, "26", from)
	assert.Equal(t, "FluxGuidance", n.ClassType)
	assert.Equal(t, "6", inputOf(g, "26", "guidance"))
	from, _ = linked(t, g, "26", "conditioning")
	assert.Equal(t, "44", from)
	from, _ = linked(t, g, "44", "clip")
	assert.Equal(t, "11", from)
	from, _ = linked(t, g, "17", "model")
	assert.Equal(t, "12", from, "the sigmas come from the loader as in the official example")
	for input, want := range map[string]string{"noise": "25", "guider": "22", "sampler": "16", "sigmas": "17", "latent_image": "45"} {
		from, _ = linked(t, g, "13", input)
		assert.Equal(t, want, from, input)
	}
	from, n = linked(t, g, "73", "samples")
	assert.Equal(t, "13", from)
	assert.Equal(t, "SamplerCustomAdvanced", n.ClassType)
	tpl, _ := localcfg.TemplateByID(id)
	caps, ok := tpl.Model.VideoCaps()
	require.True(t, ok)
	assert.False(t, caps.FirstFrame)
	assert.Equal(t, 24, tpl.Model.Workflow.FPS)
	assert.False(t, tpl.Model.Workflow.Has(localcfg.ParamNegativePrompt), "the guidance of Hunyuan has no negative prompt")
	assert.True(t, tpl.Model.Workflow.Has(localcfg.ParamCFG), "the cfg of the screen is FluxGuidance's guidance")
}
