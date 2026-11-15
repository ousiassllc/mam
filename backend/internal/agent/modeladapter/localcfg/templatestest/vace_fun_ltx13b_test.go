package templatestest

import (
	"math"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

// handoverStep is the first step of the simple scheduler whose sigma is below the boundary: the steps before it belong to the high noise expert
// (Wan runs the expert for the timesteps t >= boundary * 1000). The sigmas of ModelSamplingSD3 with a shift are shift*s / (1 + (shift-1)*s), and the
// simple scheduler of ComfyUI reads the 1000-entry table at int(x*1000/steps) from the noisy end.
func handoverStep(steps int, shift, boundary float64) int {
	for x := range steps {
		s := float64(1000-int(float64(x)*1000/float64(steps))) / 1000
		if shift*s/(1+(shift-1)*s) < boundary {
			return x
		}
	}
	return steps
}

// Wan 2.2 VACE Fun A14B (#1014) has no Comfy template, so the graph is the Wan 2.2 14B T2V template (two UNETLoader → ModelSamplingSD3 → two
// KSamplerAdvanced) with the WanVaceToVideo → TrimVideoLatent of the Wan 2.1 VACE templates, and the values are the model author's (VideoX-Fun):
// shift 12, cfg 6, the Flow (Euler) scheduler, the expert switch at the boundary 0.875. The switch is a step, so it is tested against the boundary.
func TestWan22VACEFunTemplatesCarryTheAuthorsValues(t *testing.T) {
	for _, tc := range []struct {
		id  string
		ref bool
	}{{"txt2video-wan22-vace-fun-14b", false}, {"ref2video-wan22-vace-fun-14b", true}} {
		g := graphOf(t, tc.id)
		assert.Equal(t, `"wan2.2_fun_vace_high_noise_14B_fp8_scaled.safetensors"`, str(g, "95", "unet_name"), tc.id)
		assert.Equal(t, `"wan2.2_fun_vace_low_noise_14B_fp8_scaled.safetensors"`, str(g, "96", "unet_name"), tc.id)
		assert.Equal(t, `"wan_2.1_vae.safetensors"`, str(g, "39", "vae_name"), tc.id+": WanVaceToVideo encodes in the Wan 2.1 latent format")
		assert.Empty(t, idsOf(g, "LoraLoaderModelOnly"), tc.id)
		assert.Equal(t, []string{"49"}, idsOf(g, "WanVaceToVideo"), tc.id)
		assert.NotContains(t, g["49"].Inputs, "control_video", tc.id+": no control video (mam has no such input)")
		for node, unet := range map[string]string{"54": "95", "55": "96"} {
			assert.Equal(t, "12", str(g, node, "shift"), tc.id)
			from, _ := linked(t, g, node, "model")
			assert.Equal(t, unet, from, tc.id)
		}
		for node, model := range map[string]string{"3": "54", "4": "55"} {
			from, _ := linked(t, g, node, "model")
			assert.Equal(t, model, from, tc.id+" sampler "+node)
			assert.Equal(t, "20", str(g, node, "steps"), tc.id)
			assert.Equal(t, "6", str(g, node, "cfg"), tc.id)
			assert.Equal(t, `"euler"`, str(g, node, "sampler_name"), tc.id)
			assert.Equal(t, `"simple"`, str(g, node, "scheduler"), tc.id)
			for input, want := range map[string]string{"positive": "49", "negative": "49"} {
				from, _ = linked(t, g, node, input)
				assert.Equal(t, want, from, tc.id+" "+input)
			}
		}
		from, _ := linked(t, g, "4", "latent_image")
		assert.Equal(t, "3", from, tc.id+": the low noise expert continues the high noise one's latent")
		assert.Equal(t, `"enable"`, str(g, "3", "return_with_leftover_noise"), tc.id)
		assert.Equal(t, `"disable"`, str(g, "4", "add_noise"), tc.id)
		from, _ = linked(t, g, "58", "samples")
		assert.Equal(t, "4", from, tc.id)
		trim, _ := linked(t, g, "58", "trim_amount")
		assert.Equal(t, "49", trim, tc.id)

		tpl, _ := localcfg.TemplateByID(tc.id)
		w := tpl.Model.Workflow
		assert.Equal(t, 16, w.FPS, tc.id)
		assert.Equal(t, 4, w.FramesStep, tc.id)
		assert.Equal(t, []string{"480p", "720p"}, tpl.Model.Video.Resolutions, tc.id)
		assert.Equal(t, tc.ref, w.Count(localcfg.ParamReferenceImage) == 1, tc.id)
		if tc.ref {
			from, _ = linked(t, g, "49", "reference_image")
			assert.Equal(t, "73", from)
		} else {
			assert.Empty(t, idsOf(g, "LoadImage"), tc.id)
		}

		// The switch: the baked 13 of 20 steps is the boundary 0.875 of shift 12, the handover ratio gives the same step at the default
		// and is within a step of the boundary at the other counts a user can choose.
		assert.Equal(t, strconv.Itoa(handoverStep(20, 12, 0.875)), str(g, "3", "end_at_step"), tc.id)
		assert.Equal(t, str(g, "3", "end_at_step"), str(g, "4", "start_at_step"), tc.id)
		ratios := 0
		for _, a := range w.Assignments {
			if a.Ratio > 0 {
				ratios++
				assert.InDelta(t, 0.65, a.Ratio, 0, tc.id)
				for _, steps := range []int{10, 20, 30, 40, 50} {
					assert.LessOrEqual(t, math.Abs(math.Round(float64(steps)*a.Ratio)-float64(handoverStep(steps, 12, 0.875))), 1.0, tc.id+" at "+strconv.Itoa(steps)+" steps")
				}
			}
		}
		assert.Equal(t, 2, ratios, tc.id+": end_at_step of the first sampler and start_at_step of the second")
	}
}

// LTX-Video 13B 0.9.8 Distilled (#1014): Lightricks' 13B workflow needs custom nodes, so the graph is the core-node low-level distilled one (LTXVPreprocess
// → LTXVImgToVideo → CFGGuider → SamplerCustomAdvanced with ManualSigmas) with the values of the 13B workflow and of the config
// ltxv-13b-0.9.8-distilled-fp8.yaml: the 8 distilled sigmas, cfg 1 (stg_scale 0: no STG), euler_ancestral, 24fps, image strength 0.8 and compression 30.
func TestLTX13BTemplatesAreTheCoreNodeDistilledWorkflow(t *testing.T) {
	for _, tc := range []struct {
		id    string
		image bool
	}{{"txt2video-ltx-13b-distilled", false}, {"img2video-ltx-13b-distilled", true}} {
		g := graphOf(t, tc.id)
		assert.Equal(t, `"ltxv-13b-0.9.8-distilled-fp8.safetensors"`, str(g, "44", "ckpt_name"), tc.id)
		assert.Equal(t, `"ltxv"`, str(g, "38", "type"), tc.id)
		assert.Equal(t, `"t5xxl_fp8_e4m3fn.safetensors"`, str(g, "38", "clip_name"), tc.id)
		assert.JSONEq(t, `"1.0, 0.9937, 0.9875, 0.9812, 0.975, 0.9094, 0.725, 0.4219, 0.0"`, str(g, "100", "sigmas"), tc.id)
		assert.Equal(t, `"euler_ancestral"`, str(g, "97", "sampler_name"), tc.id)
		assert.Equal(t, "1", str(g, "99", "cfg"), tc.id)
		for _, gone := range []string{"STGGuiderAdvanced", "LTXVBaseSampler", "LTXVTiledSampler", "LTXQ8Patch", "LTXVLatentUpsampler", "LTXVScheduler", "LoraLoaderModelOnly"} {
			assert.Empty(t, idsOf(g, gone), tc.id+": "+gone+" is not in this workflow")
		}
		for input, want := range map[string]string{"noise": "102", "guider": "99", "sampler": "97", "sigmas": "100"} {
			from, _ := linked(t, g, "101", input)
			assert.Equal(t, want, from, tc.id+" sampler "+input)
		}
		decode, _ := linked(t, g, "8", "samples")
		assert.Equal(t, "101", decode, tc.id)
		guided, _ := linked(t, g, "99", "positive")
		assert.Equal(t, "69", guided, tc.id+": the guider reads the conditioning with the frame rate")

		tpl, _ := localcfg.TemplateByID(tc.id)
		caps, ok := tpl.Model.VideoCaps()
		require.True(t, ok, tc.id)
		assert.False(t, caps.NativeAudio, tc.id)
		assert.False(t, caps.LastFrame, tc.id)
		assert.Equal(t, tc.image, caps.FirstFrame, tc.id)
		assert.Equal(t, 24, tpl.Model.Workflow.FPS, tc.id)
		assert.Equal(t, 8, tpl.Model.Workflow.FramesStep, tc.id)
		assert.Equal(t, 32, tpl.Model.Workflow.SizeMultiple, tc.id)

		if !tc.image {
			latent, _ := linked(t, g, "101", "latent_image")
			assert.Equal(t, "84", latent, tc.id)
			assert.Empty(t, idsOf(g, "LoadImage"), tc.id)
			continue
		}
		assert.Equal(t, "30", str(g, "82", "img_compression"), tc.id)
		assert.Equal(t, "0.8", str(g, "95", "strength"), tc.id)
		from, _ := linked(t, g, "82", "image")
		assert.Equal(t, "78", from, tc.id)
		from, _ = linked(t, g, "95", "image")
		assert.Equal(t, "82", from, tc.id+": the preprocessed frame, not the raw one")
		latent, _ := linked(t, g, "101", "latent_image")
		assert.Equal(t, "95", latent, tc.id)
		cond, _ := linked(t, g, "69", "positive")
		assert.Equal(t, "95", cond, tc.id+": the conditioning of the image node, as the official low-level workflow")
	}
}
