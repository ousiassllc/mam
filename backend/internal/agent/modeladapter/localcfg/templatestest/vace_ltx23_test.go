package templatestest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

// Wan 2.1 VACE 1.3B (#1014) as Comfy-Org's templates video_wan_vace_14B_t2v / _ref2v build their 1.3B branch: UNETLoader → ModelSamplingSD3
// (shift 8) → WanVaceToVideo → KSampler (uni_pc/simple, the "default" 20 steps and cfg 6 of the template's own note, no CausVid LoRA) →
// TrimVideoLatent by the node's trim output → VAEDecode. Text to video gives the node no reference; the reference workflow gives it one image.
func TestWan21VACETemplatesFollowTheOfficialOnes(t *testing.T) {
	for _, tc := range []struct {
		id  string
		ref bool
	}{{"txt2video-wan21-vace-1-3b", false}, {"ref2video-wan21-vace-1-3b", true}} {
		g := graphOf(t, tc.id)
		assert.Equal(t, `"wan2.1_vace_1.3B_fp16.safetensors"`, str(g, "106", "unet_name"), tc.id)
		assert.Equal(t, `"wan"`, str(g, "38", "type"), tc.id)
		assert.Equal(t, "8", str(g, "48", "shift"), tc.id)
		assert.Equal(t, []string{"49"}, idsOf(g, "WanVaceToVideo"), tc.id)
		assert.Empty(t, idsOf(g, "LoraLoader"), tc.id+": no CausVid LoRA")
		for input, want := range map[string]string{"positive": "6", "negative": "7", "vae": "105"} {
			from, _ := linked(t, g, "49", input)
			assert.Equal(t, want, from, tc.id+" "+input)
		}
		assert.NotContains(t, g["49"].Inputs, "control_video", tc.id+": no control video (mam has no such input)")
		assert.NotContains(t, g["49"].Inputs, "control_masks", tc.id)
		for input, want := range map[string]string{"model": "48", "positive": "49", "negative": "49", "latent_image": "49"} {
			from, _ := linked(t, g, "3", input)
			assert.Equal(t, want, from, tc.id+" sampler "+input)
		}
		assert.Equal(t, "20", str(g, "3", "steps"), tc.id)
		assert.Equal(t, "6", str(g, "3", "cfg"), tc.id)
		assert.Equal(t, `"uni_pc"`, str(g, "3", "sampler_name"), tc.id)
		trimFrom, _ := linked(t, g, "58", "trim_amount")
		assert.Equal(t, "49", trimFrom, tc.id+": the reference frame is trimmed off by the node's own count")
		from, _ := linked(t, g, "58", "samples")
		assert.Equal(t, "3", from, tc.id)
		from, _ = linked(t, g, "8", "samples")
		assert.Equal(t, "58", from, tc.id)

		tpl, _ := localcfg.TemplateByID(tc.id)
		assert.Equal(t, 16, tpl.Model.Workflow.FPS, tc.id)
		assert.Equal(t, 4, tpl.Model.Workflow.FramesStep, tc.id)
		assert.Equal(t, []string{"480p"}, tpl.Model.Video.Resolutions, tc.id+": 1.3B is 480p only")
		assert.Equal(t, tc.ref, tpl.Model.Workflow.Count(localcfg.ParamReferenceImage) == 1, tc.id)
		if tc.ref {
			from, _ = linked(t, g, "49", "reference_image")
			assert.Equal(t, "73", from)
			assert.Equal(t, []string{"73"}, idsOf(g, "LoadImage"))
		} else {
			assert.Empty(t, idsOf(g, "LoadImage"), tc.id)
			assert.NotContains(t, g["49"].Inputs, "reference_image", tc.id)
		}
	}
}

// LTX-2.3 (#1014): the loaders are the three of Comfy's LTX-2.3 FLF2V template (CheckpointLoaderSimple for the model and the video VAE,
// LTXVAudioVAELoader and LTXAVTextEncoderLoader reading the same checkpoint), one pass with the 8 distilled sigmas, cfg 1 and no upscaler.
// T2V has no image, I2V puts the first frame in with LTXVImgToVideoInplace (0.7), FLF2V is the 2.5 one-pass graph with two guides.
func TestLTX23TemplatesAreTheOnePassDistilledWorkflowWithAudio(t *testing.T) {
	const ckpt = `"ltx-2.3-22b-distilled-fp8.safetensors"`
	for id, wantFrames := range map[string][2]bool{"txt2video-ltx-2-3": {false, false}, "img2video-ltx-2-3": {true, false}, "flf2v-ltx-2-3": {true, true}} {
		g := graphOf(t, id)
		tpl, _ := localcfg.TemplateByID(id)
		caps, ok := tpl.Model.VideoCaps()
		require.True(t, ok, id)
		assert.True(t, caps.NativeAudio, id)
		assert.Equal(t, wantFrames, [2]bool{caps.FirstFrame, caps.LastFrame}, id)
		assert.Equal(t, 25, tpl.Model.Workflow.FPS, id)
		assert.Equal(t, 8, tpl.Model.Workflow.FramesStep, id)
		assert.Equal(t, 32, tpl.Model.Workflow.SizeMultiple, id, "one pass: the size is the latent's multiple of 32")

		for _, class := range []string{"CheckpointLoaderSimple", "LTXVAudioVAELoader", "LTXAVTextEncoderLoader"} {
			ids := idsOf(g, class)
			require.Len(t, ids, 1, id+" "+class)
			assert.Equal(t, ckpt, str(g, ids[0], "ckpt_name"), id+" "+class)
		}
		assert.Equal(t, `"gemma_3_12B_it_fp4_mixed.safetensors"`, str(g, idsOf(g, "LTXAVTextEncoderLoader")[0], "text_encoder"), id)
		for _, gone := range []string{"UNETLoader", "CLIPLoader", "VAELoader", "LatentUpscaleModelLoader", "LTXVLatentUpsampler", "LTXVDualCFGGuider"} {
			assert.Empty(t, idsOf(g, gone), id+": "+gone+" is the 2.5 graph")
		}
		samplers := idsOf(g, "SamplerCustomAdvanced")
		require.Len(t, samplers, 1, id)
		guider, _ := linked(t, g, samplers[0], "guider")
		assert.Equal(t, "CFGGuider", g[guider].ClassType, id)
		assert.Equal(t, "1", str(g, guider, "cfg"), id)
		sigmas, _ := linked(t, g, samplers[0], "sigmas")
		assert.JSONEq(t, `"1.0, 0.99375, 0.9875, 0.98125, 0.975, 0.909375, 0.725, 0.421875, 0.0"`, str(g, sigmas, "sigmas"), id)
		video, _ := linked(t, g, idsOf(g, "SaveVideo")[0], "video")
		_, audio := linked(t, g, video, "audio")
		assert.Equal(t, "LTXVAudioVAEDecode", audio.ClassType, id)
	}
	g := graphOf(t, "img2video-ltx-2-3")
	inplace := idsOf(g, "LTXVImgToVideoInplace")
	require.Len(t, inplace, 1)
	assert.Equal(t, "0.7", str(g, inplace[0], "strength"))
	assert.Empty(t, idsOf(graphOf(t, "txt2video-ltx-2-3"), "LoadImage"))
	assert.Len(t, idsOf(graphOf(t, "flf2v-ltx-2-3"), "LTXVAddGuide"), 2)
}
