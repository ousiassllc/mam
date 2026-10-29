package mmtest

import (
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/hf/hftest"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

// The model behind the Hugging Face login that WithGatedModel adds: m-g, one file of the gated repository GatedRepo at GatedCommit
// (the fake Hugging Face answers 401 / 403 until its Token is stored and Granted is set), and its license needs the consent.
const (
	GatedModelID = "m-g"
	GatedRepo    = "Lightricks/LTX-2.5"
	GatedCommit  = "49b093a39c19169a8a81721dcc541e07e945232f"
)

// GatedBody is the file of m-g.
var GatedBody = []byte(strings.Repeat("G", 120_000))

// GatedFile is the file of m-g as the fake serves it.
var GatedFile = File{"vae", "g.safetensors", GatedBody}

// GatedLicense is the license of m-g (its ID and version are what an acceptance carries).
var GatedLicense = modelmanager.LicenseRef{ID: "gated-license", Version: "1"}

// WithGatedModel adds m-g to the catalog (a model with requires_hf_token) and its gated repository to the fake Hugging Face. A
// test that wants the token to reach the fake adds HFAuth too.
func WithGatedModel() Option {
	return func(d *modelmanager.Deps, r *Rig) {
		r.HF.Add(hftest.Repo{ID: GatedRepo, Commit: GatedCommit, Gated: true, Files: []hftest.File{{Path: "vae/" + GatedFile.Name, Body: GatedBody}}})
		d.Catalog.Models = append(d.Catalog.Models, catalog.Model{
			ID: GatedModelID, Name: "G", Kind: localcfg.MediaImage, Tier: catalog.TierAdvanced, Description: "d", RequiresHFToken: true, ConsentRequired: true,
			VRAM:    catalog.VRAM{MinGB: 8, Source: "test", Estimate: true},
			License: catalog.License{ID: GatedLicense.ID, Version: GatedLicense.Version, Name: "gated", URL: "https://example.com/gated", Summary: "s", CommercialUse: true, CommercialCondition: "c"},
			Files: []catalog.File{{
				Name: GatedFile.Name, Folder: GatedFile.Folder, URL: r.HF.URL + "/" + GatedRepo + "/resolve/" + GatedCommit + "/vae/" + GatedFile.Name,
				SHA256: GatedFile.sha(), Size: int64(len(GatedBody)),
			}},
			Workflows: []catalog.Workflow{{Name: "wf-g", Template: "txt2img-sdxl"}},
		})
	}
}
