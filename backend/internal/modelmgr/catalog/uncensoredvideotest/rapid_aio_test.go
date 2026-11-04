package uncensoredvideotest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/traits"
)

const rapidAIOI2V = "wan22-rapid-aio-v10-nsfw-i2v"

// Phr00t's Rapid AllInOne I2V v10 NSFW (Issue #1012, added on 2026-11-04 after the candidates were checked again): the medium tier, first frame
// only, one checkpoint with everything in it, pinned to the commit and to the SHA-256 and the size Hugging Face reports for the file.
func TestRapidAIOI2VIsAMediumFirstFrameModelPinnedToTheFile(t *testing.T) {
	m := model(t, rapidAIOI2V)
	assert.True(t, m.Adult())
	assert.True(t, m.Traits.Uncensored)
	assert.Equal(t, catalog.TierAdvanced, m.Tier)
	assert.True(t, m.ConsentRequired)
	assert.False(t, m.RequiresHFToken)
	assert.InDelta(t, 14, m.Traits.ParamsB, 1e-9)
	assert.Equal(t, "fast", string(m.Traits.Speed))
	assert.True(t, m.VRAM.Estimate)
	assert.Contains(t, m.VRAM.Source, "未確認")
	d := m.Derived()
	assert.True(t, d.Has(traits.I2V))
	assert.True(t, d.Has(traits.FirstFrame))
	assert.False(t, d.Has(traits.T2V))
	assert.False(t, d.Audio)
	for _, want := range []string{"【中（〜14B）", "I2V", "音声なし", "得意", "注意", "メンテ", "deprecated", "macOS（Apple Silicon）は未確認"} {
		assert.Contains(t, m.Description, want)
	}
	assert.Nil(t, m.Unsupported, "no report names the checkpoint, so the macOS column stays empty like the Mega merge's")

	require.Len(t, m.Files, 1)
	f := m.Files[0]
	assert.Equal(t, "wan2.2-i2v-rapid-aio-v10-nsfw.safetensors", f.Name)
	assert.Equal(t, "checkpoints", f.Folder)
	assert.Equal(t, "https://huggingface.co/Phr00t/WAN2.2-14B-Rapid-AllInOne/resolve/30e803babc766d791f140b197aa7bf7f18f6e36d/v10/wan2.2-i2v-rapid-aio-v10-nsfw.safetensors", f.URL)
	assert.Equal(t, "3ead1d9bfd24cc650052dcd310be32bcfd0dd6175418323c41a9208bbc027081", f.SHA256)
	assert.EqualValues(t, 23387046339, f.Size)

	require.Len(t, m.Workflows, 1)
	assert.Equal(t, "img2video-wan22-14b-rapid-aio", m.Workflows[0].Template)
}

// Like the Mega merge it comes from, the license is the card's declaration only, the merged adult LoRAs are of unknown origin, and the author stopped
// maintaining it: the commercial use is unknown and the notice says it.
func TestRapidAIOI2VLicenseIsUnknown(t *testing.T) {
	l := model(t, rapidAIOI2V).License
	assert.Equal(t, modellicense.Unknown, l.Commercial)
	assert.NoError(t, l.Info().Check())
	assert.Equal(t, "2026-11-04", l.CheckedAt)
	assert.Contains(t, l.Summary, "多数の成人向け LoRA")
	assert.Contains(t, l.Notice, "商用には使わないでください")
	assert.Contains(t, l.Notice, "メンテを終了")
	require.GreaterOrEqual(t, len(l.Components), 2)
}
