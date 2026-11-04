package uncensoredvideotest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/traits"
)

// Issue #1028 (2026-11-04): the Mega merge gets its first-frame mode (a second workflow on the same checkpoint, no new weights), and two models whose
// blocker was resolved are added: NSFW Wan 14B (a full fine-tune by the author of the 1.3B model, fp8) and WAN22.XX Palingenesis with the NSFW UMT5 text
// encoder in the standard format. The files are pinned to what Hugging Face reports (LFS oid, X-Linked-Size, X-Repo-Commit); the 14B fp8 file, the text
// encoder and the Palingenesis high noise file were also hashed with sha256sum after the download.
func TestMegaGetsAFirstFrameWorkflowOnTheSameCheckpoint(t *testing.T) {
	m := model(t, "wan22-rapid-mega-nsfw-t2v")
	require.Len(t, m.Files, 1, "no new weights")
	d := m.Derived()
	assert.True(t, d.Has(traits.T2V))
	assert.True(t, d.Has(traits.I2V))
	assert.True(t, d.Has(traits.FirstFrame))
	assert.False(t, d.Has(traits.LastFrame), "the two-frame mode is not offered")
	assert.Contains(t, m.Description, "T2V と I2V")
	assert.Contains(t, m.Description, "WanVideoVACEStartToEndFrame")
	assert.Equal(t, modellicense.Unknown, m.License.Commercial, "the merged adult LoRAs named in the file's own header have no license")
	assert.Contains(t, m.License.Summary, "ファイルのヘッダー")
	_, ok := localcfg.TemplateByID("img2video-wan22-14b-rapid-mega")
	assert.True(t, ok)
	// the v10 I2V names its merged LoRAs the same way
	assert.Contains(t, model(t, rapidAIOI2V).License.Summary, "ファイルのヘッダー")
}

func TestNSFWWan14BIsAPinnedFullFineTuneRunByTheSmallModelsTemplate(t *testing.T) {
	m := model(t, "nsfw-wan21-14b-t2v")
	assert.True(t, m.Adult())
	assert.True(t, m.Traits.Uncensored)
	assert.Equal(t, catalog.TierAdvanced, m.Tier)
	assert.True(t, m.ConsentRequired)
	assert.False(t, m.RequiresHFToken)
	assert.InDelta(t, 14, m.Traits.ParamsB, 1e-9)
	assert.Equal(t, "14B", m.Traits.ParamsLabel)
	assert.Equal(t, 16, m.VRAM.MinGB)
	assert.True(t, m.VRAM.Estimate)
	assert.Contains(t, m.VRAM.Source, "未確認")
	d := m.Derived()
	assert.True(t, d.Has(traits.T2V))
	assert.False(t, d.Has(traits.I2V))
	assert.False(t, d.Audio)
	for _, want := range []string{"【中（〜14B）", "T2V", "音声なし", "得意", "注意", "条件つき"} {
		assert.Contains(t, m.Description, want)
	}
	require.NotNil(t, m.Unsupported, "fp8 weights: the MPS report applies")

	f := file(t, m, "nsfw_wan_14b_e15_fp8.safetensors")
	assert.Equal(t, "diffusion_models", f.Folder)
	assert.Equal(t, "https://huggingface.co/NSFW-API/NSFW_Wan_14b/resolve/0fce98c7e7c9876d13b717ff317001596275f2b7/nsfw_wan_14b_e15_fp8.safetensors", f.URL)
	assert.Equal(t, "bc13ced6d1f3f6df423834432f11f2efb5bbb832d81c74fb6fcebe919968216c", f.SHA256)
	assert.EqualValues(t, 14288641704, f.Size)
	small := model(t, "nsfw-wan21-1b3-t2v")
	assert.Equal(t, small.Files[1:], m.Files[1:], "the text encoder and the VAE of the 1.3B model: one copy on disk")

	// the workflow is the 1.3B model's template (the official Wan 2.1 T2V example) with the 14B file in its UNETLoader
	require.Len(t, m.Workflows, 1)
	assert.Equal(t, "txt2video-wan21-1b3-nsfw", m.Workflows[0].Template)
	wm, err := m.Workflows[0].Model(localcfg.TemplateByID)
	require.NoError(t, err)
	g, err := localcfg.ParseGraph(wm.Workflow.Graph)
	require.NoError(t, err)
	assert.JSONEq(t, `"nsfw_wan_14b_e15_fp8.safetensors"`, string(g["37"].Inputs["unet_name"]))

	l := m.License
	assert.Equal(t, "creativeml-openrail-m-declared", l.ID)
	assert.Equal(t, modellicense.Conditional, l.Commercial)
	assert.NotEmpty(t, l.CommercialCondition)
	assert.Equal(t, "2026-11-04", l.CheckedAt)
	assert.NoError(t, l.Info().Check())
	assert.Contains(t, l.Summary, "Steal this model!")
	assert.Contains(t, l.Notice, "商用で使う前に")
}

func TestPalingenesisUsesTheStandardFormatNSFWTextEncoderAndIsUnknownForCommercialUse(t *testing.T) {
	m := model(t, "wan22-t2v-14b-palingenesis")
	assert.True(t, m.Adult())
	assert.True(t, m.Traits.Uncensored, "the text encoder is the NSFW one")
	assert.Equal(t, catalog.TierAdvanced, m.Tier)
	assert.True(t, m.ConsentRequired)
	assert.False(t, m.RequiresHFToken)
	assert.InDelta(t, 14, m.Traits.ParamsB, 1e-9)
	assert.True(t, m.VRAM.Estimate)
	assert.Contains(t, m.VRAM.Source, "未確認")
	d := m.Derived()
	assert.True(t, d.Has(traits.T2V))
	assert.False(t, d.Has(traits.FirstFrame))
	assert.False(t, d.Audio)
	for _, want := range []string{"【中（〜14B）", "T2V", "音声なし", "得意", "注意", "不明", "標準の CLIPLoader が認識しない"} {
		assert.Contains(t, m.Description, want)
	}

	const commit = "https://huggingface.co/eddy1111111/WAN22.XX_Palingenesis/resolve/a97d5b718ac4a0407eb086905f41126ca9ff95cf/"
	for _, p := range []struct {
		name, url, sha string
		size           int64
	}{
		{"WAN22.XX_Palingenesis_high_t2v_final.safetensors", commit + "WAN22.XX_Palingenesis_high_t2v_final.safetensors", "c8a7cf9cb3b3d71f4845a94e8a9c1e62b6fa95b153ad366e9a2633b18938b696", 14535618242},
		{"WAN22.XX_Palingenesis_low_t2v_final.safetensors", commit + "WAN22.XX_Palingenesis_low_t2v_final.safetensors", "8e84fc87262b13aa6d773c8d958ef978dc4e571da0673843bdf340830bebbc30", 14535618242},
		{"nsfw_wan_umt5-xxl_bf16_fixed2.safetensors", "https://huggingface.co/rzgar/NSFW-Wan-UMT5-XXL-Fixed/resolve/32e7da298391ff5ee8715dc8860edc62ba0fba45/nsfw_wan_umt5-xxl_bf16_fixed2.safetensors", "0fc38a37e6cece8269c0c42440820c1fb6d5c220d22538fd0a9224a1cec697a4", 11366399657},
	} {
		f := file(t, m, p.name)
		assert.Equal(t, p.url, f.URL, p.name)
		assert.Equal(t, p.sha, f.SHA256, p.name)
		assert.Equal(t, p.size, f.Size, p.name)
	}
	assert.Empty(t, m.Files[0].SourceNote)
	std, _ := catalog.Default().Model("wan22-t2v-14b")
	for _, f := range m.Files {
		assert.NotEqual(t, "umt5_xxl_fp8_e4m3fn_scaled.safetensors", f.Name, "the official text encoder is replaced, not added")
	}
	assert.Equal(t, std.Files[3], m.Files[3], "the Wan 2.1 VAE is the official one")

	require.Len(t, m.Workflows, 1)
	assert.Equal(t, "txt2video-wan22-14b-palingenesis", m.Workflows[0].Template)

	l := m.License
	assert.Equal(t, modellicense.Unknown, l.Commercial)
	assert.Equal(t, "2026-11-04", l.CheckedAt)
	assert.NoError(t, l.Info().Check())
	assert.Contains(t, l.Notice, "商用には使わないでください")
	require.Len(t, l.Components, 4)
	assert.Equal(t, modellicense.Yes, l.Components[0].Commercial)
	assert.Equal(t, modellicense.Unknown, l.Components[1].Commercial, "a card declaration without text or training notes")
	assert.Equal(t, modellicense.Unknown, l.Components[2].Commercial)
	assert.Equal(t, modellicense.Yes, l.Components[3].Commercial)
}
