package lightmodelstest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/traits"
)

// Wan 2.2 S2V 14B (Issue #1153, added from the skipped table of #1014): every pin is the LFS oid and the size of the Hugging Face API (tree of the commit) and
// the x-linked-etag / x-linked-size of a HEAD of the pinned URL, 2026-11-15. The audio encoder goes to the folder audio_encoders.
func TestWan22S2VIsAPinnedMiddleApacheModelWithASoundWorkflow(t *testing.T) {
	m, base := model(t, "wan22-s2v-14b"), model(t, "wan22-t2v-14b")
	const commit = "https://huggingface.co/Comfy-Org/Wan-2.2/resolve/ee6f4a40737a995bf5818954cfce6d59443b0f04/split_files/"
	for name, want := range map[string]struct {
		folder, path, sha string
		size              int64
	}{
		"wan2.2_s2v_14B_fp8_scaled.safetensors":   {"diffusion_models", "diffusion_models/", "140e75af5534ac3d91e710d9df756f7032addd64b341ba2c1c70e3e6da9aa216", 16394832474},
		"wav2vec2_large_english_fp16.safetensors": {"audio_encoders", "audio_encoders/", "f0017a43ea57ef6b3d4866be607844bbd8cada6d30966f7d70044ed0d63d3f9e", 630997322},
	} {
		f := fileOf(t, m, name)
		assert.Equal(t, want.folder, f.Folder, name)
		assert.Equal(t, commit+want.path+name, f.URL, name)
		assert.Equal(t, want.sha, f.SHA256, name)
		assert.Equal(t, want.size, f.Size, name)
		assert.Empty(t, f.SourceNote, name+": not a mirror")
	}
	for _, name := range []string{"umt5_xxl_fp8_e4m3fn_scaled.safetensors", "wan_2.1_vae.safetensors"} {
		assert.Equal(t, fileOf(t, base, name), fileOf(t, m, name), name+" is the same file as the other Wan 2.2 models'")
	}
	assert.Len(t, m.Files, 4)
	assert.Equal(t, catalog.TierStandard, m.Tier)
	assert.Equal(t, catalog.RatingGeneral, m.ContentRating)
	assert.False(t, m.Traits.Uncensored)
	assert.False(t, m.ConsentRequired, "Apache-2.0: nothing to accept (and mam never accepts for the user)")
	assert.False(t, m.RequiresHFToken)
	assert.Equal(t, "apache-2.0", m.License.ID)
	assert.Equal(t, modellicense.Yes, m.License.Commercial)
	assert.Equal(t, "2026-11-15", m.License.CheckedAt)
	assert.True(t, m.VRAM.Estimate, "no published VRAM figure for ComfyUI")
	require.NotNil(t, m.Unsupported, "fp8 weights stop in the MPS backend")
	assert.Equal(t, []string{"darwin"}, m.Unsupported.OS)
	assert.Equal(t, []traits.VideoMode{traits.ModeS2V, traits.ModeR2V}, m.Traits.VideoModes)
	assert.True(t, m.Derived().AudioInput)
	assert.False(t, m.Derived().Audio, "the sound is an input: the model makes no sound of its own")
	require.Len(t, m.Workflows, 1)
	assert.Equal(t, "audio2video-wan22-s2v-14b", m.Workflows[0].Template)
	assert.Contains(t, m.Description, "中ティア")
	assert.Contains(t, m.Description, "音声ファイル")
	assert.Contains(t, m.Description, "実機での生成は未確認")
}
