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

// The two standard (not uncensored) video models Issue #1014 added in its second step: Wan 2.1 VACE 1.3B (small) and LTX-2.3 22B Distilled
// (large). Every pin is what the Hugging Face API (the LFS oid and the size) and a HEAD of the pinned URL (x-linked-etag, x-linked-size)
// returned on 2026-11-04, with the file at the commit of its repository.
func TestVACEAndLTX23ArePinnedGeneralModels(t *testing.T) {
	for id, pins := range map[string]map[string]struct {
		url, sha string
		size     int64
	}{
		"wan21-vace-1-3b": {
			"wan2.1_vace_1.3B_fp16.safetensors": {"https://huggingface.co/Comfy-Org/Wan-2.1/resolve/123acf1cc74bccbb9bfff8ac1ee72edc08c2341d/split_files/diffusion_models/wan2.1_vace_1.3B_fp16.safetensors", "640ccc0577e6a5d4bb15cd91b11b699ef914fc55f126c5a1c544e152130784f2", 4309519800},
		},
		"ltx-2.3-22b-distilled": {
			"ltx-2.3-22b-distilled-fp8.safetensors": {"https://huggingface.co/Lightricks/LTX-2.3-fp8/resolve/91eda216f820e5ecb3d17dc9fe3b7cdaf56b587d/ltx-2.3-22b-distilled-fp8.safetensors", "d9646b6f2d5c42d337b23671634c43bfeece6989644f51b4a3aa088465ccd3b2", 29531884062},
			"gemma_3_12B_it_fp4_mixed.safetensors":  {"https://huggingface.co/Comfy-Org/ltx-2/resolve/ccde4ba417d7900669fd56dd292a883cee11ff37/split_files/text_encoders/gemma_3_12B_it_fp4_mixed.safetensors", "aaca463d11e6d8d2a4bdb0d6299214c15ef78a3f73e0ef8113d5a9d0219b3f6d", 9447702218},
		},
	} {
		m := model(t, id)
		assert.Equal(t, catalog.RatingGeneral, m.ContentRating, id)
		assert.False(t, m.Traits.Uncensored, id)
		assert.Equal(t, "2026-11-04", m.License.CheckedAt, id)
		assert.False(t, m.RequiresHFToken, id+": neither repository is gated")
		for name, want := range pins {
			f := fileOf(t, m, name)
			assert.Equal(t, want.url, f.URL, id+" "+name)
			assert.Equal(t, want.sha, f.SHA256, id+" "+name)
			assert.Equal(t, want.size, f.Size, id+" "+name)
			assert.Empty(t, f.SourceNote, id+" "+name+": not a mirror")
		}
	}
}

// VACE 1.3B is the small tier (Apache-2.0, no consent) and shares the encoder and the VAE with the other Wan 2.1 / 2.2 models; it has a text
// to video and a reference image to video workflow, and no first frame: the reference is a look, not the first frame of the clip.
func TestWan21VACEIsASmallApacheModelWithTwoWorkflows(t *testing.T) {
	m, base := model(t, "wan21-vace-1-3b"), model(t, "wan22-i2v-14b")
	assert.Equal(t, catalog.TierStandard, m.Tier)
	assert.False(t, m.ConsentRequired)
	assert.Equal(t, "apache-2.0", m.License.ID)
	assert.Equal(t, modellicense.Yes, m.License.Commercial)
	assert.InDelta(t, 1.3, m.Traits.ParamsB, 0)
	for _, name := range []string{"umt5_xxl_fp8_e4m3fn_scaled.safetensors", "wan_2.1_vae.safetensors"} {
		assert.Equal(t, fileOf(t, base, name), fileOf(t, m, name), name+" is the same file as the other Wan models'")
	}
	require.Len(t, m.Workflows, 2)
	for _, w := range m.Workflows {
		caps, ok := mustTemplate(t, w.Template).Model.VideoCaps()
		require.True(t, ok, w.Name)
		assert.False(t, caps.FirstFrame, w.Name)
		assert.False(t, caps.LastFrame, w.Name)
		assert.Equal(t, map[string]int{"wan21-vace-1-3b-t2v": 0, "wan21-vace-1-3b-r2v": 1}[w.Name], caps.MaxReferenceImages, w.Name)
	}
	assert.Contains(t, m.Description, "小ティア")
	assert.Contains(t, m.Description, "VACE")
	assert.Contains(t, m.Description, "音声なし")
}

// LTX-2.3 is the large tier with sound: the LTX-2 Community License (conditional on revenue) and the Gemma Terms of Use of the text encoder both
// ask for the consent, a conditional answer for the whole; it has the three workflows (T2V, I2V, FLF2V) and the model files of the 2.3 checkpoint.
func TestLTX23IsAConditionalLicenseModelWithSoundAndThreeWorkflows(t *testing.T) {
	m := model(t, "ltx-2.3-22b-distilled")
	assert.Equal(t, catalog.TierAdvanced, m.Tier)
	assert.True(t, m.ConsentRequired, "accepting is the user's act at the install: nothing accepts for them")
	assert.Equal(t, modellicense.Conditional, m.License.Commercial)
	assert.Contains(t, m.License.Notice, "licensing@ltx.io")
	assert.Contains(t, m.License.URL, "LICENSE-2", "the 2.3 card links LICENSE-2, not the LICENSE-2_x of 2.5")
	assert.NotContains(t, m.License.URL, "LICENSE-2_x")
	require.Len(t, m.License.Components, 2)
	assert.Equal(t, "Gemma Terms of Use", m.License.Components[1].Name)
	assert.True(t, m.Derived().Audio)
	assert.Equal(t, []traits.Input{traits.T2V, traits.I2V, traits.FirstFrame, traits.LastFrame}, m.Derived().Inputs)
	require.Len(t, m.Workflows, 3)
	for _, w := range m.Workflows {
		assert.True(t, strings.HasPrefix(w.Name, "ltx-2-3-"), w.Name)
		assert.True(t, mustTemplate(t, w.Template).Model.Video.NativeAudio, w.Name)
	}
	assert.Contains(t, m.Description, "大ティア")
	assert.Contains(t, m.Description, "音声")
}
