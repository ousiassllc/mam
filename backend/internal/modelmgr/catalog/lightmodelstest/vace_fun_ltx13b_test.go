package lightmodelstest_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/traits"
)

// The two models Issue #1014 took from its skipped table on 2026-11-14: Wan 2.2 VACE Fun A14B and LTX-Video 13B 0.9.8 Distilled. Every pin is the
// LFS oid and the size of the Hugging Face API (tree of the commit) and the x-linked-etag / x-linked-size of a HEAD of the pinned URL, 2026-11-14.
func TestVACEFunAndLTX13BArePinnedGeneralModels(t *testing.T) {
	for id, pins := range map[string]map[string]struct {
		url, sha string
		size     int64
	}{
		"wan22-vace-fun-14b": {
			"wan2.2_fun_vace_high_noise_14B_fp8_scaled.safetensors": {"https://huggingface.co/Comfy-Org/Wan-2.2/resolve/ee6f4a40737a995bf5818954cfce6d59443b0f04/split_files/diffusion_models/wan2.2_fun_vace_high_noise_14B_fp8_scaled.safetensors", "23130f30207f6c8697a5bead113ddc9904cfbb5e8fa51a550df3cee4d21fb54c", 17346056104},
			"wan2.2_fun_vace_low_noise_14B_fp8_scaled.safetensors":  {"https://huggingface.co/Comfy-Org/Wan-2.2/resolve/ee6f4a40737a995bf5818954cfce6d59443b0f04/split_files/diffusion_models/wan2.2_fun_vace_low_noise_14B_fp8_scaled.safetensors", "ca55a5cc543e28576edbd2079f8c1de05f71cd9e85bf96fab635f55663dbcb69", 17346056104},
		},
		"ltxv-13b-0.9.8-distilled": {
			"ltxv-13b-0.9.8-distilled-fp8.safetensors": {"https://huggingface.co/Lightricks/LTX-Video/resolve/8984fa25007f376c1a299016d0957a37a2f797bb/ltxv-13b-0.9.8-distilled-fp8.safetensors", "111a3d07baa17f520e98b571e7916139ae0865c9a24b7534529d6b9e74264db3", 15694280140},
		},
	} {
		m := model(t, id)
		assert.Equal(t, catalog.RatingGeneral, m.ContentRating, id)
		assert.False(t, m.Traits.Uncensored, id)
		assert.Equal(t, "2026-11-14", m.License.CheckedAt, id)
		assert.False(t, m.RequiresHFToken, id+": neither repository is gated")
		assert.True(t, m.VRAM.Estimate, id+": no published VRAM figure")
		for name, want := range pins {
			f := fileOf(t, m, name)
			assert.Equal(t, want.url, f.URL, id+" "+name)
			assert.Equal(t, want.sha, f.SHA256, id+" "+name)
			assert.Equal(t, want.size, f.Size, id+" "+name)
			assert.Empty(t, f.SourceNote, id+" "+name+": not a mirror")
		}
		require.NotNil(t, m.Unsupported, id+": fp8 weights stop in the MPS backend")
		assert.Equal(t, []string{"darwin"}, m.Unsupported.OS, id)
	}
}

// VACE Fun is the middle tier (Apache-2.0 by the card of the model, no consent), shares the encoder and the VAE with the other Wan 2.2 models, has a
// text to video and a reference image to video workflow and no first frame (the reference is a look, not the first frame of the clip).
func TestWan22VACEFunIsAMiddleApacheModelWithTwoWorkflows(t *testing.T) {
	m, base := model(t, "wan22-vace-fun-14b"), model(t, "wan22-t2v-14b")
	assert.Equal(t, catalog.TierStandard, m.Tier)
	assert.False(t, m.ConsentRequired)
	assert.Equal(t, "apache-2.0", m.License.ID)
	assert.Equal(t, modellicense.Yes, m.License.Commercial)
	assert.Equal(t, []traits.VideoMode{traits.ModeT2V, traits.ModeVACE, traits.ModeR2V}, m.Traits.VideoModes)
	for _, name := range []string{"umt5_xxl_fp8_e4m3fn_scaled.safetensors", "wan_2.1_vae.safetensors"} {
		assert.Equal(t, fileOf(t, base, name), fileOf(t, m, name), name+" is the same file as the other Wan 2.2 models'")
	}
	assert.Len(t, m.Files, 4, "the two experts, the encoder and the VAE: the Wan 2.2 VAE is not the one of VACE")
	require.Len(t, m.Workflows, 2)
	for _, w := range m.Workflows {
		caps, ok := mustTemplate(t, w.Template).Model.VideoCaps()
		require.True(t, ok, w.Name)
		assert.False(t, caps.FirstFrame, w.Name)
		assert.False(t, caps.LastFrame, w.Name)
		assert.False(t, caps.NativeAudio, w.Name)
		assert.Equal(t, map[string]int{"wan22-vace-fun-14b-t2v": 0, "wan22-vace-fun-14b-r2v": 1}[w.Name], caps.MaxReferenceImages, w.Name)
	}
	assert.Contains(t, m.Description, "中ティア")
	assert.Contains(t, m.Description, "VACE")
	assert.Contains(t, m.Description, "音声なし")
	assert.Contains(t, m.Description, "ComfyUI の公式のテンプレートは無く", "the caution says the workflow is the model author's values, not Comfy's template")
}

// LTX-Video 13B uses the LTXV Open Weights License 0.X: free below US$10 million of annual revenue, a paid license above, which is conditional, so
// it asks for the consent (an advanced-tier entry, as LTX-2.3 is) and carries the condition and the address of the notice. The text encoder is the file
// ltx-video-2b already installs. Two workflows: text, and a first frame; the end frame of the official keyframe workflow needs custom nodes.
func TestLTX13BIsAConditionalLicenseModelWithTwoWorkflows(t *testing.T) {
	m, base := model(t, "ltxv-13b-0.9.8-distilled"), model(t, "ltx-video-2b")
	assert.Equal(t, catalog.TierAdvanced, m.Tier)
	assert.True(t, m.ConsentRequired, "accepting is the user's act at the install: nothing accepts for them")
	assert.Equal(t, modellicense.Conditional, m.License.Commercial)
	assert.Contains(t, m.License.CommercialCondition, "1,000 万米ドル")
	assert.Contains(t, m.License.Notice, "ltxv-licensing@lightricks.com")
	assert.Contains(t, m.License.URL, "LTX-Video-Open-Weights-License-0.X.txt")
	require.Len(t, m.License.Components, 2)
	assert.Equal(t, modellicense.Conditional, m.License.Components[0].Commercial)
	assert.Equal(t, modellicense.Yes, m.License.Components[1].Commercial, "T5-XXL is Apache-2.0")
	assert.Equal(t, fileOf(t, base, "t5xxl_fp8_e4m3fn.safetensors"), fileOf(t, m, "t5xxl_fp8_e4m3fn.safetensors"))
	assert.Equal(t, []traits.Input{traits.T2V, traits.I2V, traits.FirstFrame}, m.Derived().Inputs)
	assert.False(t, m.Derived().Audio)
	assert.Equal(t, []traits.VideoMode{traits.ModeT2V, traits.ModeI2V}, m.Traits.VideoModes)
	require.Len(t, m.Workflows, 2)
	for _, w := range m.Workflows {
		caps, ok := mustTemplate(t, w.Template).Model.VideoCaps()
		require.True(t, ok, w.Name)
		assert.False(t, caps.LastFrame, w.Name)
		assert.Equal(t, strings.HasSuffix(w.Name, "-i2v"), caps.FirstFrame, w.Name)
	}
	assert.Contains(t, m.Description, "中ティア")
	assert.Contains(t, m.Description, "音声なし")
	assert.Contains(t, m.Description, "1,000 万米ドル")
}
