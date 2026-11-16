package templatestest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

// Wan 2.2 S2V 14B (#1153): the graph is the 20 step side of Comfy's video_wan2_2_14B_s2v template (core nodes only, no lightx2v LoRA), with the
// image and the sound as inputs of the run. The wiring is what makes the sound drive the clip, so it is tested edge by edge.
func TestWan22S2VTemplateIsTheOfficialTwentyStepGraph(t *testing.T) {
	g := graphOf(t, "audio2video-wan22-s2v-14b")
	assert.Equal(t, `"wan2.2_s2v_14B_fp8_scaled.safetensors"`, str(g, "37", "unet_name"))
	assert.Equal(t, `"wav2vec2_large_english_fp16.safetensors"`, str(g, "57", "audio_encoder_name"))
	assert.Equal(t, `"wan_2.1_vae.safetensors"`, str(g, "39", "vae_name"))
	assert.Empty(t, idsOf(g, "LoraLoaderModelOnly"), "the 4 step LoRA is not this workflow")
	assert.Empty(t, idsOf(g, "WanSoundImageToVideoExtend"), "the graph is one chunk: the Extend is audio_chunks.extension, added per run")
	assert.Equal(t, "8", str(g, "54", "shift"))
	assert.Equal(t, "20", str(g, "3", "steps"))
	assert.Equal(t, "6", str(g, "3", "cfg"))
	assert.Equal(t, `"uni_pc"`, str(g, "3", "sampler_name"))
	assert.Equal(t, `"simple"`, str(g, "3", "scheduler"))

	for _, e := range []struct{ node, input, from string }{
		{"56", "audio", "58"}, {"56", "audio_encoder", "57"}, // the sound is encoded by wav2vec2
		{"93", "audio_encoder_output", "56"}, {"93", "ref_image", "52"}, // the conditioning takes the encoded sound and the picture
		{"3", "positive", "93"}, {"3", "negative", "93"}, {"3", "latent_image", "93"}, {"3", "model", "54"},
		{"54", "model", "37"}, {"80", "samples", "3"}, {"96", "image", "80"}, {"82", "images", "96"}, // the decoder's frames are cut to the sound's length before the video

		{"82", "audio", "58"}, // the saved video carries the input sound, as the official template does
		{"113", "video", "82"},
	} {
		from, _ := linked(t, g, e.node, e.input)
		assert.Equal(t, e.from, from, e.node+"."+e.input)
	}

	tpl, _ := localcfg.TemplateByID("audio2video-wan22-s2v-14b")
	caps, ok := tpl.Model.VideoCaps()
	require.True(t, ok)
	assert.True(t, caps.AudioInput)
	assert.Equal(t, 1, caps.MaxReferenceImages, "the picture of the speaker")
	assert.False(t, caps.FirstFrame)
	assert.False(t, caps.NativeAudio, "the sound is an input, not made by the model")
	assert.True(t, caps.AudioLength, "the clip is as long as the sound")
	assert.Equal(t, []float64{5}, caps.Durations, "informative only: the first chunk is 5 s")
	assert.Equal(t, []string{"reference", "audio"}, tpl.Model.Workflow.RequiredImages())
	assert.Equal(t, 77, tpl.Model.Workflow.AudioChunks.ChunkLength, "the model needs at least 73 and 77 is the official chunk")
	assert.Equal(t, "77", str(g, "93", "length"), "the graph's first chunk is a whole chunk")
}
