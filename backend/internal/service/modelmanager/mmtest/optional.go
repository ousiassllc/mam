package mmtest

import (
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

// OptionalModelID is the model WithOptionalModel adds (#1025).
const OptionalModelID = "m-opt"

// WithOptionalModel adds m-opt to the catalog: two required files (the text encoder and the VAE of the test catalog) and one optional one (the LoRA LoraD).
// Its workflow is wf-opt of the SDXL template.
func WithOptionalModel() Option {
	return func(d *modelmanager.Deps, r *Rig) {
		src := d.Catalog.Models[0]
		d.Catalog.Models = append(d.Catalog.Models, catalog.Model{
			ID: OptionalModelID, Name: "Opt", Kind: localcfg.MediaImage, Tier: catalog.TierStandard, Description: "d", VRAM: src.VRAM, License: src.License,
			Files:     []catalog.File{r.Host.CatalogFile(ClipC, false), r.Host.CatalogFile(VaeBC, false), r.Host.CatalogFile(LoraD, true)},
			Workflows: []catalog.Workflow{{Name: "wf-opt", Template: "txt2img-sdxl"}},
		})
	}
}
