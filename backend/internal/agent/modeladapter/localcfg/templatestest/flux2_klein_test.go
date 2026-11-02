package templatestest

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

// FLUX.2 klein 4B (Issue #931) as ComfyUI's official templates build it (image_flux2_klein_text_to_image and
// image_flux2_klein_image_edit_4b_distilled): the fp8 distilled model, the Qwen3 4B text encoder as a flux2 CLIP, 4 steps of
// Flux2Scheduler with euler and a cfg of 1; and one slot (image, scaler, VAEEncode, two ReferenceLatent nodes) for each of up to
// four references, so that the same workflow is the text-to-image one and the editing one.
func TestKleinTemplateFollowsTheOfficialWorkflow(t *testing.T) {
	g := graphOf(t, "txt2img-flux2-klein")
	str := func(id, input string) string { return string(g[id].Inputs[input]) }

	assert.Equal(t, `"flux-2-klein-4b-fp8.safetensors"`, str("37", "unet_name"))
	assert.Equal(t, `"qwen_3_4b.safetensors"`, str("38", "clip_name"))
	assert.Equal(t, `"flux2"`, str("38", "type"), "the CLIP type of FLUX.2")
	assert.Equal(t, `"flux2-vae.safetensors"`, str("39", "vae_name"))
	assert.Equal(t, "4", str("62", "steps"))
	assert.Equal(t, "1", str("63", "cfg"))
	assert.Equal(t, `"euler"`, str("61", "sampler_name"))
	assert.Equal(t, []string{"7"}, idsOf(g, "ConditioningZeroOut"))

	assert.Equal(t, []string{"101", "102", "103", "104"}, idsOf(g, "LoadImage"))
	assert.Len(t, idsOf(g, "ReferenceLatent"), 8, "a pair for each reference")
	for k, slot := range [][2]string{{"131", "141"}, {"132", "142"}, {"133", "143"}, {"134", "144"}} {
		for _, rl := range slot {
			from, n := linked(t, g, rl, "latent")
			assert.Equal(t, "VAEEncode", n.ClassType)
			assert.Equal(t, fmt.Sprintf("12%d", k+1), from)
			scaler, sn := linked(t, g, from, "pixels")
			assert.Equal(t, "ImageScaleToTotalPixels", sn.ClassType)
			load, _ := linked(t, g, scaler, "image")
			assert.Equal(t, fmt.Sprintf("10%d", k+1), load)
		}
	}

	tpl, _ := localcfg.TemplateByID("txt2img-flux2-klein")
	caps := tpl.Model.ReferenceCaps()
	assert.Equal(t, localcfg.ModeEdit, caps.Mode)
	assert.Equal(t, 4, caps.Max)
	assert.Equal(t, []string{localcfg.RoleWhole, localcfg.RoleFace, localcfg.RoleStyle, localcfg.RoleEdit}, caps.Roles)
	require.True(t, tpl.Model.ImageInputs(localcfg.KindComfyUI))
}
