package vramfit_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/vramfit"
)

// LTX-2.5 is the first model whose recommendation (32 GB, Lightricks) is above what it runs on (16 GB, quantized): a GPU between the two
// is "動くが制約あり", and the RTX 4070 Ti SUPER (16376 MiB, which is 15.99 GiB) counts as the 16 GB it is sold as.
func TestLTX25IsLimitedOnA16GBGPU(t *testing.T) {
	m, ok := catalog.Default().Model("ltx-2.5-22b-distilled")
	require.True(t, ok)
	assert.Equal(t, 16, m.VRAM.MinGB)
	assert.Equal(t, 32, m.VRAM.RecommendedGB)
	assert.Contains(t, m.VRAM.Limits, "INT8")
	assert.Contains(t, m.VRAM.Limits, "タイル VAE")
	assert.Contains(t, m.VRAM.Limits, "オフロード")
	assert.True(t, m.VRAM.Estimate, "nothing was run on mam's own GPU")

	mib := func(n uint64) uint64 { return n << 20 }
	for name, tc := range map[string]struct {
		vram uint64
		want vramfit.Fit
	}{
		"no GPU memory known":    {0, vramfit.Unknown},
		"RTX 3060 12 GB":         {mib(12288), vramfit.Short},
		"RTX 4070 Ti SUPER 16 G": {mib(16376), vramfit.Limited},
		"RTX 4090 24 GB":         {mib(24564), vramfit.Limited},
		"RTX 5090 32 GB":         {mib(32607), vramfit.Enough},
	} {
		assert.Equal(t, tc.want, vramfit.For(m.VRAM, tc.vram), name)
	}
	assert.Empty(t, vramfit.Text(m.VRAM, 0))
	assert.Contains(t, vramfit.Text(m.VRAM, mib(16376)), "VRAM 16 GB")
	assert.Contains(t, vramfit.Text(m.VRAM, mib(16376)), "動くが制約あり")
	assert.Contains(t, vramfit.Text(m.VRAM, mib(16376)), m.VRAM.Limits)
	assert.Contains(t, vramfit.Text(m.VRAM, mib(12288)), "動かない可能性が高い")

	// A model with no higher recommendation is "enough" from its minimum on.
	plain := catalog.VRAM{MinGB: 12, Source: "s"}
	assert.Equal(t, vramfit.Enough, vramfit.For(plain, mib(12288)))
	assert.Equal(t, vramfit.Short, vramfit.For(plain, mib(8192)))
}
