// Package mainfile gives a workflow template another main model file. The SDXL templates and the Qwen-Image 2.1 template are shared by
// many catalog models, each with its own file (catalog.Workflow.ModelFile): the install and the catalog validation both take the
// model of the template with that file in place of the template's own.
package mainfile

import (
	"encoding/json"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

// loaders are the core loader nodes (and the input) that name the main file of a model: the checkpoint of an all-in-one
// checkpoint model, the diffusion model of a split one (UNETLoader; UnetLoaderGGUF of the custom node ComfyUI-GGUF for a GGUF).
var loaders = map[string]string{"CheckpointLoaderSimple": "ckpt_name", "UNETLoader": "unet_name", "UnetLoaderGGUF": "unet_name"}

// Model is the model of the template with the file of its main loader replaced by file. The template is not changed. The template
// must have exactly one main loader (loaders), or it does not say which node the file is for.
func Model(tpl localcfg.Template, file string) (localcfg.Model, error) {
	m := tpl.Model
	if m.Workflow == nil {
		return localcfg.Model{}, fmt.Errorf("template %s has no workflow", tpl.ID)
	}
	g, err := localcfg.ParseGraph(m.Workflow.Graph)
	if err != nil {
		return localcfg.Model{}, fmt.Errorf("template %s: %w", tpl.ID, err)
	}
	loader := ""
	for id, n := range g {
		if _, ok := loaders[n.ClassType]; !ok {
			continue
		}
		if loader != "" {
			return localcfg.Model{}, fmt.Errorf("template %s has more than one main loader, model_file cannot say which", tpl.ID)
		}
		loader = id
	}
	if loader == "" {
		return localcfg.Model{}, fmt.Errorf("template %s has no main loader (CheckpointLoaderSimple, UNETLoader, UnetLoaderGGUF) for model_file", tpl.ID)
	}
	value, err := json.Marshal(file)
	if err != nil {
		return localcfg.Model{}, err
	}
	n := g[loader]
	inputs := make(map[string]json.RawMessage, len(n.Inputs))
	for k, v := range n.Inputs {
		inputs[k] = v
	}
	inputs[loaders[n.ClassType]] = value
	n.Inputs = inputs
	g[loader] = n
	graph, err := json.Marshal(g)
	if err != nil {
		return localcfg.Model{}, err
	}
	wf := *m.Workflow
	wf.Graph = graph
	m.Workflow = &wf
	return m, nil
}
