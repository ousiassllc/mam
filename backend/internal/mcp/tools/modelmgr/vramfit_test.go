package modelmgr_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/tools/modelmgr"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
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
	env, rig := newEnv(t, withRecommendation())

	var unknown modelmgr.ListCatalogOutput
	env.CallOK("list_model_catalog", map[string]any{}, &unknown)
	assert.Empty(t, unknown.VRAMFit, "no GPU memory is known")

	rig.GPUReport.Adapters = []gpu.Adapter{{Name: "NVIDIA GeForce RTX 4070 Ti SUPER", VRAMBytes: 16376 << 20}}
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
