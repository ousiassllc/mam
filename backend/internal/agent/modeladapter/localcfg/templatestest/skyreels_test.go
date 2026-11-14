package templatestest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

// SkyReels V2 I2V 1.3B 540P (#1014): the graph is Comfy's own Wan 2.1 I2V (template image_to_video_wan: CLIPVisionEncode with crop none → WanImageToVideo
// → one KSampler with uni_pc/simple, ModelSamplingSD3 before it), and the values are the ones SkyReels-V2's card and generate_video.py give for the I2V
// model: 544x960, 97 frames, 24 fps, guidance 5, shift 3 (not the 8 of its T2V), 30 steps.
func TestSkyReelsV2TemplateIsTheWanI2VGraphWithTheModelsOwnValues(t *testing.T) {
	const id = "img2video-skyreels-v2-1-3b"
	g := graphOf(t, id)
	assert.Equal(t, `"skyreels_v2_i2v_1.3B_540P_fp32.safetensors"`, str(g, "37", "unet_name"))
	assert.Equal(t, `"wan"`, str(g, "38", "type"))
	assert.Equal(t, "3", str(g, "54", "shift"), "I2V shift 3; 8 is the T2V one")
	assert.Equal(t, "5", str(g, "3", "cfg"), "I2V guidance_scale 5; 6 is the T2V one")
	assert.Equal(t, "30", str(g, "3", "steps"))
	assert.Equal(t, `"uni_pc"`, str(g, "3", "sampler_name"))
	assert.Equal(t, "960", str(g, "50", "width"))
	assert.Equal(t, "544", str(g, "50", "height"))
	assert.Equal(t, "97", str(g, "50", "length"))
	assert.Equal(t, `"none"`, str(g, "51", "crop"))
	assert.Equal(t, []string{"50"}, idsOf(g, "WanImageToVideo"))
	for input, want := range map[string]string{"positive": "6", "negative": "7", "vae": "39", "clip_vision_output": "51", "start_image": "52"} {
		from, _ := linked(t, g, "50", input)
		assert.Equal(t, want, from, "WanImageToVideo "+input)
	}
	for input, want := range map[string]string{"model": "54", "positive": "50", "negative": "50", "latent_image": "50"} {
		from, _ := linked(t, g, "3", input)
		assert.Equal(t, want, from, "sampler "+input)
	}
	from, _ := linked(t, g, "54", "model")
	assert.Equal(t, "37", from, "the shift is applied to the model the loader gives")
	clipImage, _ := linked(t, g, "51", "image")
	assert.Equal(t, "52", clipImage, "the CLIP Vision encodes the same first frame the node starts from")
	assert.Equal(t, []string{"52"}, idsOf(g, "LoadImage"), "a first frame and no other image")

	tpl, _ := localcfg.TemplateByID(id)
	assert.Equal(t, 24, tpl.Model.Workflow.FPS)
	assert.Equal(t, 4, tpl.Model.Workflow.FramesStep, "97 frames is 4n+1")
	assert.Equal(t, []string{"540p"}, tpl.Model.Video.Resolutions)
	w, h := localcfg.VideoSize("540p", "16:9", tpl.Model.Workflow.SizeMultiple)
	assert.Equal(t, [2]int{960, 544}, [2]int{w, h}, "the 540p of 16:9 is the 544x960 the card names")
	caps, ok := tpl.Model.VideoCaps()
	require.True(t, ok)
	assert.True(t, caps.FirstFrame)
	assert.False(t, caps.LastFrame)
	assert.Equal(t, 1, tpl.Model.Workflow.Count(localcfg.ParamFirstFrame))
	assert.Zero(t, tpl.Model.Workflow.Count(localcfg.ParamReferenceImage))
}
