package templatestest

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Qwen-Image-Edit 2511 (Issue #971) as the blueprint "Image Edit (Qwen 2511)" of the bundled ComfyUI (v0.42.1) builds it: the model
// goes through ModelSamplingAuraFlow (shift 3.1) and CFGNorm, the positive and the negative conditioning are both made by
// TextEncodeQwenImageEditPlus with the same references, and the sampler is the official reference's 20 steps, cfg 4, euler/simple.
func TestQwenImageEdit2511TemplateFollowsTheOfficialBlueprint(t *testing.T) {
	g := graphOf(t, "txt2img-qwen-image-edit-2511")
	str := func(id, input string) string { return string(g[id].Inputs[input]) }

	assert.Equal(t, `"qwen_image_edit_2511_fp8mixed.safetensors"`, str("37", "unet_name"))
	assert.Equal(t, `"qwen_2.5_vl_7b_fp8_scaled.safetensors"`, str("38", "clip_name"))
	assert.Equal(t, `"qwen_image"`, str("38", "type"))
	assert.Equal(t, `"qwen_image_vae.safetensors"`, str("39", "vae_name"))
	assert.Equal(t, "3.1", str("66", "shift"))
	assert.Equal(t, "1", str("75", "strength"))
	assert.Equal(t, "20", str("3", "steps"))
	assert.Equal(t, "4", str("3", "cfg"))
	assert.Equal(t, `"euler"`, str("3", "sampler_name"))
	assert.Equal(t, `"simple"`, str("3", "scheduler"))

	from, n := linked(t, g, "75", "model")
	assert.Equal(t, "66", from)
	assert.Equal(t, "ModelSamplingAuraFlow", n.ClassType)
	from, n = linked(t, g, "66", "model")
	assert.Equal(t, "37", from)
	assert.Equal(t, "UNETLoader", n.ClassType)
	from, _ = linked(t, g, "3", "model")
	assert.Equal(t, "75", from, "the sampler takes the model after CFGNorm")

	assert.Equal(t, []string{"6", "7"}, idsOf(g, "TextEncodeQwenImageEditPlus"))
	assert.Equal(t, []string{"101", "102", "103"}, idsOf(g, "LoadImage"))
	for _, enc := range []string{"6", "7"} {
		for i, load := range []string{"101", "102", "103"} {
			from, _ := linked(t, g, enc, []string{"image1", "image2", "image3"}[i])
			assert.Equal(t, load, from, "encoder %s image%d", enc, i+1)
		}
		from, _ := linked(t, g, enc, "vae")
		assert.Equal(t, "39", from)
	}
	from, _ = linked(t, g, "3", "positive")
	assert.Equal(t, "6", from)
	from, _ = linked(t, g, "3", "negative")
	assert.Equal(t, "7", from)
}
