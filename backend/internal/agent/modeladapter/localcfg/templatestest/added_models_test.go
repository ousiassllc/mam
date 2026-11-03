package templatestest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

// Krea 2 Turbo (#984, LUSTIFY v10) as Comfy-Org's template image_krea2_turbo_t2i builds it: UNETLoader, a CLIPLoader of type krea2, the
// Qwen-Image VAE, 8 steps and cfg 1 with euler/simple, and the conditioning zeroed for the negative side (there is no negative prompt).
func TestKrea2TurboTemplateFollowsTheOfficialTemplate(t *testing.T) {
	g := graphOf(t, "txt2img-krea2-turbo")
	str := func(id, input string) string { return string(g[id].Inputs[input]) }
	assert.Equal(t, `"krea2"`, str("11", "type"))
	assert.Equal(t, `"qwen3vl_4b_fp8_scaled.safetensors"`, str("11", "clip_name"))
	assert.Equal(t, `"qwen_image_vae.safetensors"`, str("12", "vae_name"))
	assert.Equal(t, "8", str("3", "steps"))
	assert.Equal(t, "1", str("3", "cfg"))
	assert.Equal(t, `"euler"`, str("3", "sampler_name"))
	assert.Equal(t, `"simple"`, str("3", "scheduler"))
	from, n := linked(t, g, "13", "conditioning")
	assert.Equal(t, "6", from)
	assert.Equal(t, "CLIPTextEncode", n.ClassType)
	from, _ = linked(t, g, "3", "negative")
	assert.Equal(t, "13", from, "the negative side is the positive conditioning zeroed out")
	from, _ = linked(t, g, "6", "clip")
	assert.Equal(t, "11", from)

	tpl, _ := localcfg.TemplateByID("txt2img-krea2-turbo")
	assert.False(t, tpl.Model.ImageInputs(localcfg.KindComfyUI), "text to image only")
}

// Rapid-AIO is one checkpoint (model, CLIP and VAE in it): the loader feeds the encoders, the sampler and the decoder, the author's 4 steps
// and cfg 1 are the defaults, and the references are the three of the standard EditPlus encoder.
func TestRapidAIOTemplateTakesEverythingFromTheCheckpoint(t *testing.T) {
	g := graphOf(t, "txt2img-qwen-image-edit-rapid-aio")
	assert.Equal(t, []string{"4"}, idsOf(g, "CheckpointLoaderSimple"))
	assert.Empty(t, idsOf(g, "UNETLoader"))
	assert.Empty(t, idsOf(g, "CLIPLoader"))
	assert.Empty(t, idsOf(g, "VAELoader"))
	for _, enc := range []string{"6", "7"} {
		from, _ := linked(t, g, enc, "clip")
		assert.Equal(t, "4", from)
		from, _ = linked(t, g, enc, "vae")
		assert.Equal(t, "4", from)
	}
	from, _ := linked(t, g, "3", "model")
	assert.Equal(t, "4", from)
	from, _ = linked(t, g, "8", "vae")
	assert.Equal(t, "4", from)
	assert.Equal(t, "4", string(g["3"].Inputs["steps"]))
	assert.Equal(t, "1", string(g["3"].Inputs["cfg"]))
	assert.Equal(t, `"euler_ancestral"`, string(g["3"].Inputs["sampler_name"]))
	assert.Equal(t, `"beta"`, string(g["3"].Inputs["scheduler"]))

	tpl, _ := localcfg.TemplateByID("txt2img-qwen-image-edit-rapid-aio")
	caps := tpl.Model.ReferenceCaps()
	assert.Equal(t, localcfg.ModeEdit, caps.Mode)
	assert.Equal(t, 3, caps.Max)
}

// The SDXL Lightning templates are the SDXL ones with the author's 5 steps, cfg 1.5 and dpmpp_sde: the same nodes and the same
// assignments, so that they take the same parameters (and a reference image in the image-to-image one).
func TestSDXLLightningTemplatesOnlyChangeTheSamplingDefaults(t *testing.T) {
	for _, kind := range []string{"txt2img", "img2img"} {
		base, light := graphOf(t, kind+"-sdxl"), graphOf(t, kind+"-sdxl-lightning")
		require.Len(t, light, len(base), kind)
		for id, n := range base {
			assert.Equal(t, n.ClassType, light[id].ClassType, kind+" "+id)
		}
		assert.Equal(t, "5", string(light["3"].Inputs["steps"]), kind)
		assert.Equal(t, "1.5", string(light["3"].Inputs["cfg"]), kind)
		assert.Equal(t, `"dpmpp_sde"`, string(light["3"].Inputs["sampler_name"]), kind)
		a, _ := localcfg.TemplateByID(kind + "-sdxl")
		b, _ := localcfg.TemplateByID(kind + "-sdxl-lightning")
		assert.Equal(t, a.Model.Workflow.Assignments, b.Model.Workflow.Assignments, kind)
	}
}

// The VNCCS PoseStudio LoRA is in the graph (behind the diffusion model, the sampler takes the LoRA's output), at strength 1, with cfg 1 as the
// author's workflow; the official int8 diffusion model of Qwen-Image 2.1 is the one it was made for.
func TestVNCCSPoseTemplateAppliesTheLoRAAtStrengthOne(t *testing.T) {
	g := graphOf(t, "txt2img-qwen-image21-vnccs-pose")
	assert.Equal(t, `"qwen_image_2.1_int8_convrot.safetensors"`, string(g["37"].Inputs["unet_name"]))
	assert.Equal(t, []string{"50"}, idsOf(g, "LoraLoaderModelOnly"))
	assert.Equal(t, `"VNCCS_QI2_PoseStudioV1.1.safetensors"`, string(g["50"].Inputs["lora_name"]))
	assert.JSONEq(t, "1", string(g["50"].Inputs["strength_model"]))
	from, _ := linked(t, g, "50", "model")
	assert.Equal(t, "37", from)
	from, _ = linked(t, g, "3", "model")
	assert.Equal(t, "50", from)
	assert.Equal(t, "1", string(g["3"].Inputs["cfg"]))
	tpl, _ := localcfg.TemplateByID("txt2img-qwen-image21-vnccs-pose")
	assert.Equal(t, "50", tpl.Model.Workflow.LoraModelNode)
	assert.Equal(t, localcfg.ModeEdit, tpl.Model.ReferenceCaps().Mode)
}
