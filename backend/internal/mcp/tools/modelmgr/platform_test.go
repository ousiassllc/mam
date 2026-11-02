package modelmgr_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/mcp/tools/modelmgr"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/platform"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/gpu"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
)

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
