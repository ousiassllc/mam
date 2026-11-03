package templatestest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

func str(g localcfg.Graph, id, input string) string { return string(g[id].Inputs[input]) }

// Wan 2.2 T2V 14B (#1014) as Comfy-Org's template video_wan2_2_14B_t2v builds it (the 20-step branch, not the LightX2V one): the high
// noise model takes steps 0-10 and the low noise one the rest on one empty latent, each behind ModelSamplingSD3 with shift 5, cfg 3.5,
// euler/simple, and no start image (the empty latent is EmptyHunyuanLatentVideo).
func TestWan22T2VTemplateFollowsTheOfficialTemplate(t *testing.T) {
	g := graphOf(t, "txt2video-wan22-14b")
	assert.Equal(t, `"wan2.2_t2v_high_noise_14B_fp8_scaled.safetensors"`, str(g, "95", "unet_name"))
	assert.Equal(t, `"wan2.2_t2v_low_noise_14B_fp8_scaled.safetensors"`, str(g, "96", "unet_name"))
	assert.Equal(t, `"wan"`, str(g, "38", "type"))
	assert.Equal(t, []string{"59"}, idsOf(g, "EmptyHunyuanLatentVideo"))
	assert.Empty(t, idsOf(g, "WanImageToVideo"), "text to video: no start image")
	assert.Empty(t, idsOf(g, "LoadImage"))
	for id, unet := range map[string]string{"54": "95", "55": "96"} {
		from, _ := linked(t, g, id, "model")
		assert.Equal(t, unet, from)
		assert.Equal(t, "5", str(g, id, "shift"))
	}
	high, low := "3", "4"
	for _, id := range []string{high, low} {
		assert.Equal(t, "20", str(g, id, "steps"))
		assert.Equal(t, "3.5", str(g, id, "cfg"))
		assert.Equal(t, `"euler"`, str(g, id, "sampler_name"))
		assert.Equal(t, `"simple"`, str(g, id, "scheduler"))
		from, _ := linked(t, g, id, "positive")
		assert.Equal(t, "6", from)
		from, _ = linked(t, g, id, "negative")
		assert.Equal(t, "7", from)
	}
	from, _ := linked(t, g, high, "model")
	assert.Equal(t, "54", from, "the high noise model runs first")
	from, _ = linked(t, g, high, "latent_image")
	assert.Equal(t, "59", from)
	from, _ = linked(t, g, low, "model")
	assert.Equal(t, "55", from)
	from, _ = linked(t, g, low, "latent_image")
	assert.Equal(t, high, from, "the low noise sampler continues the high noise one")
	assert.Equal(t, "0", str(g, high, "start_at_step"))
	assert.Equal(t, "10", str(g, high, "end_at_step"))
	assert.Equal(t, `"enable"`, str(g, high, "return_with_leftover_noise"))
	assert.Equal(t, "10", str(g, low, "start_at_step"))
	assert.Equal(t, `"disable"`, str(g, low, "add_noise"))

	tpl, _ := localcfg.TemplateByID("txt2video-wan22-14b")
	caps, ok := tpl.Model.VideoCaps()
	require.True(t, ok)
	assert.False(t, caps.FirstFrame, "text to video")
	assert.False(t, caps.LastFrame)
}

// Wan 2.1 T2V 1.3B (#1014) as ComfyUI_examples/wan/text_to_video_wan.json builds it: one UNETLoader (fp16) into ModelSamplingSD3 (shift 8)
// and one KSampler (uni_pc/simple), on an EmptyHunyuanLatentVideo.
func TestWan21T2V13BTemplateFollowsTheOfficialExample(t *testing.T) {
	g := graphOf(t, "txt2video-wan21-1-3b")
	assert.Equal(t, `"wan2.1_t2v_1.3B_fp16.safetensors"`, str(g, "37", "unet_name"))
	assert.Equal(t, `"wan"`, str(g, "38", "type"))
	assert.Equal(t, `"wan_2.1_vae.safetensors"`, str(g, "39", "vae_name"))
	assert.Len(t, idsOf(g, "UNETLoader"), 1, "one model, not the high / low noise pair")
	assert.Len(t, idsOf(g, "KSampler"), 1)
	assert.Equal(t, "8", str(g, "48", "shift"))
	assert.Equal(t, "30", str(g, "3", "steps"))
	assert.Equal(t, "6", str(g, "3", "cfg"))
	assert.Equal(t, `"uni_pc"`, str(g, "3", "sampler_name"))
	assert.Equal(t, `"simple"`, str(g, "3", "scheduler"))
	from, _ := linked(t, g, "3", "model")
	assert.Equal(t, "48", from)
	from, _ = linked(t, g, "3", "latent_image")
	assert.Equal(t, "40", from)
	assert.Equal(t, "EmptyHunyuanLatentVideo", g["40"].ClassType)
	tpl, _ := localcfg.TemplateByID("txt2video-wan21-1-3b")
	caps, _ := tpl.Model.VideoCaps()
	assert.False(t, caps.FirstFrame, "text to video")
	assert.Equal(t, []string{"480p"}, tpl.Model.Video.Resolutions, "the author recommends 480p; 720p is less stable")
}

// Wan 2.1 FLF2V 14B (#1014) as Comfy-Org's template wan2.1_flf2v_720_f16 builds it: each of the two images goes through CLIPVisionEncode
// (crop none) and, with the images themselves, into WanFirstLastFrameToVideo; one KSampler (uni_pc, 20 steps, cfg 3) takes its outputs.
// The first_frame and last_frame assignments make the model take two images.
func TestWan21FLF2VTemplateFollowsTheOfficialTemplate(t *testing.T) {
	g := graphOf(t, "flf2v-wan21-14b")
	assert.Equal(t, `"wan2.1_flf2v_720p_14B_fp8_e4m3fn.safetensors"`, str(g, "37", "unet_name"))
	assert.Equal(t, `"clip_vision_h.safetensors"`, str(g, "49", "clip_name"))
	assert.Equal(t, `"wan"`, str(g, "38", "type"))
	assert.Equal(t, "8", str(g, "79", "shift"))
	assert.Equal(t, []string{"52", "72"}, idsOf(g, "LoadImage"), "the first and the last frame")
	for enc, img := range map[string]string{"51": "52", "87": "72"} {
		assert.Equal(t, `"none"`, str(g, enc, "crop"))
		from, _ := linked(t, g, enc, "image")
		assert.Equal(t, img, from)
		from, _ = linked(t, g, enc, "clip_vision")
		assert.Equal(t, "49", from)
	}
	for input, want := range map[string]string{"clip_vision_start_image": "51", "clip_vision_end_image": "87", "start_image": "52", "end_image": "72", "vae": "39", "positive": "6", "negative": "7"} {
		from, _ := linked(t, g, "83", input)
		assert.Equal(t, want, from, input)
	}
	assert.Equal(t, "WanFirstLastFrameToVideo", g["83"].ClassType)
	for input, out := range map[string]string{"positive": "0", "negative": "1", "latent_image": "2"} {
		from, _ := linked(t, g, "3", input)
		assert.Equal(t, "83", from, input)
		assert.Contains(t, string(g["3"].Inputs[input]), out)
	}
	from, _ := linked(t, g, "3", "model")
	assert.Equal(t, "79", from)
	assert.Equal(t, "20", str(g, "3", "steps"))
	assert.Equal(t, "3", str(g, "3", "cfg"))
	assert.Equal(t, `"uni_pc"`, str(g, "3", "sampler_name"))

	tpl, _ := localcfg.TemplateByID("flf2v-wan21-14b")
	caps, ok := tpl.Model.VideoCaps()
	require.True(t, ok)
	assert.True(t, caps.FirstFrame)
	assert.True(t, caps.LastFrame)
	assert.Equal(t, []string{"720p"}, tpl.Model.Video.Resolutions, "the model is trained for 720P only")
}

// HunyuanVideo 1.5 720p (#1014), text to video, as Comfy-Org's template video_hunyuan_video_1.5_720p_t2v builds its main branch: the
// fp16 720p model behind ModelSamplingSD3 (shift 7), CFGGuider cfg 6, 20 steps euler/simple, on a 1280x720 EmptyHunyuanVideo15Latent.
func TestHunyuan15720pT2VTemplateFollowsTheOfficialTemplate(t *testing.T) {
	g := graphOf(t, "txt2video-hunyuan15-720p")
	assert.Equal(t, `"hunyuanvideo1.5_720p_t2v_fp16.safetensors"`, str(g, "12", "unet_name"))
	assert.Equal(t, `"hunyuan_video_15"`, str(g, "11", "type"))
	assert.Equal(t, "1280", str(g, "124", "width"))
	assert.Equal(t, "720", str(g, "124", "height"))
	assert.Equal(t, "7", str(g, "132", "shift"))
	assert.Equal(t, "6", str(g, "131", "cfg"))
	assert.Equal(t, "20", str(g, "128", "steps"))
	assert.Equal(t, `"euler"`, str(g, "130", "sampler_name"))
	from, _ := linked(t, g, "131", "model")
	assert.Equal(t, "132", from)
	from, _ = linked(t, g, "127", "latent_image")
	assert.Equal(t, "124", from)
	assert.Empty(t, idsOf(g, "HunyuanVideo15ImageToVideo"))
	assert.Empty(t, idsOf(g, "HunyuanVideo15SuperResolution"), "the 1080p super resolution branch is not used")
	tpl, _ := localcfg.TemplateByID("txt2video-hunyuan15-720p")
	caps, _ := tpl.Model.VideoCaps()
	assert.False(t, caps.FirstFrame)
	assert.Equal(t, []string{"720p"}, tpl.Model.Video.Resolutions)
}

// The image to video one feeds the start image to HunyuanVideo15ImageToVideo together with the SigCLIP vision output (CLIPVisionEncode,
// crop center, as in video_hunyuan_video_1.5_720p_i2v), and the sampler takes its conditioning and its latent.
func TestHunyuan15720pI2VTemplateTakesTheStartImageAndTheClipVision(t *testing.T) {
	g := graphOf(t, "img2video-hunyuan15-720p")
	assert.Equal(t, `"hunyuanvideo1.5_720p_i2v_cfg_distilled_fp8_scaled.safetensors"`, str(g, "12", "unet_name"))
	assert.Equal(t, `"sigclip_vision_patch14_384.safetensors"`, str(g, "81", "clip_name"))
	assert.Equal(t, `"center"`, str(g, "79", "crop"))
	assert.Empty(t, idsOf(g, "EmptyHunyuanVideo15Latent"), "the latent comes from the image to video node")
	for input, want := range map[string]string{"start_image": "80", "clip_vision_output": "79", "vae": "10", "positive": "44", "negative": "93"} {
		from, _ := linked(t, g, "78", input)
		assert.Equal(t, want, from, input)
	}
	from, _ := linked(t, g, "79", "image")
	assert.Equal(t, "80", from)
	from, _ = linked(t, g, "79", "clip_vision")
	assert.Equal(t, "81", from)
	for input, out := range map[string]string{"positive": "0", "negative": "1"} {
		from, _ := linked(t, g, "131", input)
		assert.Equal(t, "78", from, input)
		assert.Contains(t, string(g["131"].Inputs[input]), out)
	}
	from, _ = linked(t, g, "127", "latent_image")
	assert.Equal(t, "78", from)
	assert.Equal(t, "1280", str(g, "78", "width"))
	assert.Equal(t, "720", str(g, "78", "height"))
	tpl, _ := localcfg.TemplateByID("img2video-hunyuan15-720p")
	caps, ok := tpl.Model.VideoCaps()
	require.True(t, ok)
	assert.True(t, caps.FirstFrame)
	assert.False(t, caps.LastFrame)
}
