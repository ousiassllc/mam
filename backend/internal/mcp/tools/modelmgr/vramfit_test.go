package modelmgr_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/tools/modelmgr"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/platform"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/gpu"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/vramfit"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
)

// withRecommendation gives m-b the memory a model like LTX-2.5 has: it runs from 16 GB, but 32 GB is recommended.
func withRecommendation() mmtest.Option {
	return func(d *modelmanager.Deps, _ *mmtest.Rig) {
		for i := range d.Catalog.Models {
			if d.Catalog.Models[i].ID == "m-b" {
				d.Catalog.Models[i].VRAM = catalog.VRAM{MinGB: 16, RecommendedGB: 32, Limits: "INT8 とオフロードが前提で遅い", Source: "test", Estimate: true}
			}
		}
	}
}

// list_model_catalog tells the AI how this PC's GPU stands to each model: 16 GB for a 32 GB recommendation is "limited" (動くが制約あり) with the
// limits in words, and nothing is said while the GPU memory is not known.
func TestCatalog_ReportsHowTheGPUStandsToEachModelsVRAM(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	env, rig := newEnv(t, withRecommendation(), mmtest.WithClock(func() time.Time { return now }))

	var unknown modelmgr.ListCatalogOutput
	env.CallOK("list_model_catalog", map[string]any{}, &unknown)
	assert.Empty(t, unknown.VRAMFit, "no GPU memory is known")

	rig.GPUReport.Adapters = []gpu.Adapter{{Name: "NVIDIA GeForce RTX 4070 Ti SUPER", VRAMBytes: 16376 << 20}}
	now = now.Add(time.Hour) // the memory of the GPU is remembered for a few minutes: a new one is read after that
	var list modelmgr.ListCatalogOutput
	env.CallOK("list_model_catalog", map[string]any{}, &list)
	require.Contains(t, list.VRAMFit, "m-b")
	assert.Equal(t, vramfit.Limited, list.VRAMFit["m-b"].Fit)
	assert.Contains(t, list.VRAMFit["m-b"].Text, "動くが制約あり")
	assert.Contains(t, list.VRAMFit["m-b"].Text, "INT8 とオフロードが前提で遅い")
	assert.NotContains(t, list.VRAMFit, "m-a", "a model with no recommendation above its minimum is not judged")
	for _, m := range list.Models {
		if m.ID == "m-b" {
			assert.Equal(t, 32, m.VRAM.RecommendedGB, "the recommendation is in the model itself")
			assert.NotEmpty(t, m.VRAM.Limits)
		}
	}
}

// list_model_catalog tells which models this PC's GPU generation is outside of (Issue #979: m-b is Blackwell only), with the columns in the model itself;
// a PC inside, or a GPU of an unknown generation, gets nothing.
func TestCatalog_ReportsTheModelsThePCIsOutsideOf(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	blackwellOnly := func(d *modelmanager.Deps, _ *mmtest.Rig) {
		for i := range d.Catalog.Models {
			if d.Catalog.Models[i].ID == "m-b" {
				d.Catalog.Models[i].Requires = &platform.Requires{GPUArch: []string{platform.ArchBlackwell}, Reason: "FP4 の演算が要ります。", Source: "test 2026-11-03"}
			}
		}
	}
	env, rig := newEnv(t, blackwellOnly, mmtest.WithClock(func() time.Time { return now }))
	list := func(cc float64) (out modelmgr.ListCatalogOutput) {
		rig.GPUReport.Adapters = []gpu.Adapter{{Name: "NVIDIA GeForce RTX", VendorID: gpu.VendorNVIDIA, VRAMBytes: 16 << 30, ComputeCap: cc}}
		now = now.Add(time.Hour) // the detection is remembered for a few minutes
		env.CallOK("list_model_catalog", map[string]any{}, &out)
		return out
	}
	out := list(8.9)
	assert.Contains(t, out.PlatformWarnings["m-b"], "NVIDIA Ada Lovelace")
	assert.Contains(t, out.PlatformWarnings["m-b"], "FP4 の演算が要ります。")
	assert.NotContains(t, out.PlatformWarnings, "m-a", "a model without the columns is not warned about")
	assert.Empty(t, list(12.0).PlatformWarnings, "Blackwell is inside")
	assert.Empty(t, list(0).PlatformWarnings, "an unknown generation is not guessed")
}
