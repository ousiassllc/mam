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
