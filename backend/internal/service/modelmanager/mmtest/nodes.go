package mmtest

import (
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/hf/hftest"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/nodes"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

// The model that needs a custom node that WithNodeModel adds: m-n, one file of an ordinary repository, and the workflow of IP-Adapter (its template
// declares requires_nodes: ipadapter-plus). WithGGUFModel adds m-g instead, whose workflow needs comfyui-gguf (#1016).
const (
	NodeModelID = "m-n"
	GGUFModelID = "m-g"
	nodeRepo    = "test/ipadapter"
	nodeCommit  = "49b093a39c19169a8a81721dcc541e07e945232f"
)

// NodeModelFile is the file of m-n.
var NodeModelFile = File{"vae", "n.safetensors", []byte(strings.Repeat("N", 90_000))}

// WithNodeModel gives the Service the custom nodes of mgr (what the engine is told to load) and adds m-n, a model whose workflow needs the node
// "ipadapter-plus", to the catalog.
func WithNodeModel(mgr *nodes.Manager) Option {
	return withNodeModel(mgr, NodeModelID, "wf-n", "txt2img-sdxl-ipadapter")
}

// WithGGUFModel is WithNodeModel for m-g, a model whose workflow (the GGUF template of Qwen-Image 2.1) needs the node "comfyui-gguf".
func WithGGUFModel(mgr *nodes.Manager) Option {
	return withNodeModel(mgr, GGUFModelID, "wf-g", "txt2img-qwen-image21-gguf")
}

func withNodeModel(mgr *nodes.Manager, id, workflow, template string) Option {
	return func(d *modelmanager.Deps, r *Rig) {
		d.Nodes = mgr
		r.HF.Add(hftest.Repo{ID: nodeRepo, Commit: nodeCommit, Files: []hftest.File{{Path: "vae/" + NodeModelFile.Name, Body: NodeModelFile.Body}}})
		d.Catalog.Models = append(d.Catalog.Models, catalog.Model{
			ID: id, Name: "N", Kind: localcfg.MediaImage, Tier: catalog.TierStandard, Description: "d",
			VRAM:    catalog.VRAM{MinGB: 8, Source: "test", Estimate: true},
			License: catalog.License{ID: "apache-2.0", Version: "2.0", Name: "Apache-2.0", URL: "https://example.com/a", Summary: "s", Commercial: modellicense.Yes, CheckedAt: "2026-11-03"},
			Files: []catalog.File{{
				Name: NodeModelFile.Name, Folder: NodeModelFile.Folder, URL: r.HF.URL + "/" + nodeRepo + "/resolve/" + nodeCommit + "/vae/" + NodeModelFile.Name,
				SHA256: NodeModelFile.sha(), Size: int64(len(NodeModelFile.Body)),
			}},
			Workflows: []catalog.Workflow{{Name: workflow, Template: template}},
		})
	}
}
