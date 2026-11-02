package catalog

import (
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/mainfile"
)

// Workflow is a #831 model created on the bundled endpoint when the model is installed: Name is its model
// name (the identifier is comfyui:bundled-comfyui/<Name>), Template the localcfg workflow template it starts from.
// ModelFile (optional) replaces the main file of the template (the CheckpointLoaderSimple checkpoint or the UNETLoader diffusion model;
// package mainfile): the SDXL and Qwen-Image 2.1 templates are shared by many models, each with its own file.
type Workflow struct {
	Name      string `json:"name"`
	Template  string `json:"template"`
	ModelFile string `json:"model_file,omitempty"`
}

// Model is the model the install saves for the workflow: the one of the template, with ModelFile in place of its main file.
func (w Workflow) Model(template func(id string) (localcfg.Template, bool)) (localcfg.Model, error) {
	tpl, ok := template(w.Template)
	switch {
	case !ok:
		return localcfg.Model{}, fmt.Errorf("template %q does not exist", w.Template)
	case w.ModelFile == "":
		return tpl.Model, nil
	}
	return mainfile.Model(tpl, w.ModelFile)
}
