package lightmodelstest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/traits"
)

// SkyReels V2 I2V 1.3B 540P (Issue #1014, 2026-11-14). The diffusion model is the repository's own model.safetensors, pinned at the repository
// commit; its SHA-256 and size are the LFS oid and size of the Hugging Face API and the x-linked-etag / x-linked-size of a HEAD of the pinned URL.
// The other three files are the Wan 2.1 ones the other Wan models of the catalog load, so they must be the same files.
func TestSkyReelsV2IsPinnedAndSharesTheWanFiles(t *testing.T) {
	m, base := model(t, "skyreels-v2-i2v-1-3b-540p"), model(t, "wan21-flf2v-14b-720p")
	f := fileOf(t, m, "skyreels_v2_i2v_1.3B_540P_fp32.safetensors")
	assert.Equal(t, "https://huggingface.co/Skywork/SkyReels-V2-I2V-1.3B-540P/resolve/e86231f3882225e5a93eeec740c77bc7f01954ca/model.safetensors", f.URL)
	assert.Equal(t, "5a513b738be37b443b50ec4a4ae7f797d4a0bdb36989ab07b40f5611153055ba", f.SHA256)
	assert.Equal(t, int64(6257814568), f.Size)
	assert.Equal(t, "diffusion_models", f.Folder)
	assert.Empty(t, f.SourceNote, "the repository of the model itself, not a mirror")
	assert.False(t, m.RequiresHFToken, "the repository is not gated")
	assert.Len(t, m.Files, 4)
	for _, name := range []string{"umt5_xxl_fp8_e4m3fn_scaled.safetensors", "clip_vision_h.safetensors", "wan_2.1_vae.safetensors"} {
		assert.Equal(t, fileOf(t, base, name), fileOf(t, m, name), name+" is the same file as the other Wan models'")
	}
}

// The license is "unknown", not "no": the card says license: other with a LICENSE file that is empty, the GitHub LICENSE.txt says commercial use is
// supported, and the Skywork Community License defines the "Skywork Model" as Skywork-13B. That is an answer that cannot be checked, and a catalog
// rule then puts the model in the advanced tier with the consent.
func TestSkyReelsV2LicenseIsUnknownAndNeedsConsent(t *testing.T) {
	m := model(t, "skyreels-v2-i2v-1-3b-540p")
	assert.Equal(t, modellicense.Unknown, m.License.Commercial)
	assert.Equal(t, "2026-11-14", m.License.CheckedAt)
	assert.Equal(t, catalog.TierAdvanced, m.Tier)
	assert.True(t, m.ConsentRequired)
	assert.False(t, m.Traits.Uncensored)
	assert.Equal(t, catalog.RatingGeneral, m.ContentRating)
	require.Len(t, m.License.Components, 2)
	assert.Equal(t, modellicense.Unknown, m.License.Components[0].Commercial)
	assert.Equal(t, modellicense.Yes, m.License.Components[1].Commercial, "the Wan encoder, CLIP Vision and VAE are Apache-2.0")
	assert.Contains(t, m.License.Notice, "商用には使わないでください")
	assert.Contains(t, m.License.Summary, "空")
	assert.Contains(t, m.License.Summary, "Skywork-13B")
}

// SkyReels V2 I2V is a plain image-to-video model: one workflow, a first frame and nothing else (no last frame, no reference images, no sound), at the 540p
// the card names; mam's durations stop at 4 s because the model's 97 frames at 24 fps are 4 s.
func TestSkyReelsV2HasOneFirstFrameWorkflow(t *testing.T) {
	m := model(t, "skyreels-v2-i2v-1-3b-540p")
	assert.Equal(t, []traits.VideoMode{traits.ModeI2V}, m.Traits.VideoModes)
	assert.InDelta(t, 1.3, m.Traits.ParamsB, 0)
	require.Len(t, m.Workflows, 1)
	assert.Equal(t, "skyreels-v2-i2v-1-3b-540p", m.Workflows[0].Name)
	caps, ok := mustTemplate(t, m.Workflows[0].Template).Model.VideoCaps()
	require.True(t, ok)
	assert.True(t, caps.FirstFrame)
	assert.False(t, caps.LastFrame)
	assert.Zero(t, caps.MaxReferenceImages)
	assert.False(t, caps.NativeAudio)
	assert.Equal(t, []string{"540p"}, caps.Resolutions)
	assert.Equal(t, []float64{2, 3, 4}, caps.Durations)
	assert.Contains(t, m.Description, "小ティア")
	assert.Contains(t, m.Description, "音声なし")
	assert.Contains(t, m.Description, "不明")
	assert.Equal(t, "darwin", m.Unsupported.OS[0], "the shared umt5 fp8 encoder fails on a Mac (ComfyUI #10292)")
}
