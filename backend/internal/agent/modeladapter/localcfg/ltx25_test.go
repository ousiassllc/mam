package localcfg_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

// The LTX-2.5 templates (Issue #877) follow the official two-pass workflow of ComfyUI (video_ltx2_5_t2v / _i2v): the first pass works
// at half of the asked size, the latent is upsampled by two and the second pass finishes it; the audio is made with the video.
func TestLTX25TemplatesAreTheTwoPassWorkflowWithAudio(t *testing.T) {
	for _, id := range []string{"txt2video-ltx-2-5", "img2video-ltx-2-5"} {
		g := graphOf(t, id)
		tpl, _ := localcfg.TemplateByID(id)
		caps, ok := tpl.Model.VideoCaps()
		require.True(t, ok, id)
		assert.True(t, caps.NativeAudio, id)
		assert.Equal(t, id == "img2video-ltx-2-5", caps.FirstFrame, id)
		assert.Equal(t, 24, tpl.Model.Workflow.FPS, id)
		assert.Equal(t, 8, tpl.Model.Workflow.FramesStep, id)
		assert.Equal(t, 64, tpl.Model.Workflow.SizeMultiple, id, "half of the size is still a multiple of the 32 of the latent")

		samplers := idsOf(g, "SamplerCustomAdvanced")
		require.Len(t, samplers, 2, id)
		upsampler := idsOf(g, "LTXVLatentUpsampler")
		require.Len(t, upsampler, 1, id)
		assert.JSONEq(t, `"ltx-2.5-latent-spatial-upscaler-x2-bf16-1.0.safetensors"`, string(g[idsOf(g, "LatentUpscaleModelLoader")[0]].Inputs["model_name"]), id)
		// the first pass starts from the empty latent at half of the size, and the upsampler takes its video latent
		sep, _ := linked(t, g, upsampler[0], "samples")
		first, _ := linked(t, g, sep, "av_latent")
		assert.Contains(t, samplers, first, id)
		for _, dim := range [][2]string{{"width", "372"}, {"height", "360"}} {
			half, n := linked(t, g, idsOf(g, "EmptyLTXVLatentVideo")[0], dim[0])
			assert.Equal(t, "ComfyMathExpression", n.ClassType, id)
			assert.JSONEq(t, `"a/2"`, string(n.Inputs["expression"]), id)
			from, _ := linked(t, g, half, "values.a")
			assert.Equal(t, dim[1], from, "%s: half of the asked %s", id, dim[0])
		}
		// the picture and the sound are decoded from the second pass and joined in one video
		video, _ := linked(t, g, idsOf(g, "SaveVideo")[0], "video")
		assert.Equal(t, "CreateVideo", g[video].ClassType, id)
		_, audio := linked(t, g, video, "audio")
		assert.Equal(t, "LTXVAudioVAEDecode", audio.ClassType, id)
		// the loaders name the files of the catalog model, the text encoder is of type ltxv
		assert.JSONEq(t, `"ltxv"`, string(g[idsOf(g, "CLIPLoader")[0]].Inputs["type"]), id)
	}
	i2v := graphOf(t, "img2video-ltx-2-5")
	assert.Len(t, idsOf(i2v, "LTXVImgToVideoInplace"), 2, "the first frame conditions both passes")
	assert.Len(t, idsOf(i2v, "LoadImage"), 1)
}

// The uncensored templates (Issue #927) are the standard ones with one difference: the text encoder file the CLIPLoader reads. The audio comes
// out the same way: SaveVideo with the automatic container (an mp4 with H.264 video and AAC audio in ComfyUI v0.42.1), so the sound is in the file
// the generation test stores and the <video> of the screen plays.
func TestLTX25UncensoredTemplatesDifferOnlyInTheTextEncoder(t *testing.T) {
	const standard, uncensored = "gemma4-12b-with-proj-ltx-2.5-comfy-int8-convrot.safetensors", "gemma4-12b-uncensored-heretic-deepneuralnerd-ltx-2.5-int8-convrot.safetensors"
	for _, kind := range []string{"txt2video", "img2video"} {
		std, _ := localcfg.TemplateByID(kind + "-ltx-2-5")
		unc, ok := localcfg.TemplateByID(kind + "-ltx-2-5-uncensored")
		require.True(t, ok, kind)
		assert.Equal(t, strings.ReplaceAll(string(std.Model.Workflow.Graph), standard, uncensored), string(unc.Model.Workflow.Graph), kind)
		assert.Equal(t, std.Model.Workflow.Assignments, unc.Model.Workflow.Assignments, kind)
		caps, ok := unc.Model.VideoCaps()
		require.True(t, ok)
		assert.True(t, caps.NativeAudio, kind)

		g := graphOf(t, kind+"-ltx-2-5-uncensored")
		save := g[idsOf(g, "SaveVideo")[0]]
		assert.JSONEq(t, `"auto"`, string(save.Inputs["format"]), kind)
		assert.JSONEq(t, `"auto"`, string(save.Inputs["codec"]), kind)
		assert.Contains(t, unc.Description, "成人向け", kind)
	}
}

// The defaults for a 16 GB GPU are in the graph itself: the quantized diffusion model and text encoder, and the tiled VAE decode.
func TestLTX25TemplatesDefaultToTheLowVRAMSetup(t *testing.T) {
	for _, id := range []string{"txt2video-ltx-2-5", "img2video-ltx-2-5", "txt2video-ltx-2-5-uncensored", "img2video-ltx-2-5-uncensored"} {
		g := graphOf(t, id)
		assert.Contains(t, string(g[idsOf(g, "UNETLoader")[0]].Inputs["unet_name"]), "int8-convrot", id)
		assert.Contains(t, string(g[idsOf(g, "CLIPLoader")[0]].Inputs["clip_name"]), "int8", id)
		require.Len(t, idsOf(g, "VAEDecodeTiled"), 1, id)
		assert.Empty(t, idsOf(g, "VAEDecode"), id+": the whole frame stack is never decoded at once")
	}
}
