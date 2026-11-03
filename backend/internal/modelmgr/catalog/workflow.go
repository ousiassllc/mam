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
	// Sampling (optional) is the steps, cfg, sampler and scheduler the model's author recommends, which the model runs with in place of the
	// template's own (a request that names a value still wins). Only what the author states goes here (the description says where), not mam's guess.
	Sampling *localcfg.SamplingSettings `json:"sampling,omitempty"`
}

// Model is the model the install saves for the workflow: the one of the template, with ModelFile in place of its main file and Sampling in
// place of its sampling values.
func (w Workflow) Model(template func(id string) (localcfg.Template, bool)) (localcfg.Model, error) {
	tpl, ok := template(w.Template)
	if !ok {
		return localcfg.Model{}, fmt.Errorf("template %q does not exist", w.Template)
	}
	m := tpl.Model
	if w.ModelFile != "" {
		var err error
		if m, err = mainfile.Model(tpl, w.ModelFile); err != nil {
			return localcfg.Model{}, err
		}
	}
	if w.Sampling == nil {
		return m, nil
	}
	m, err := m.WithSampling(*w.Sampling)
	if err != nil {
		return localcfg.Model{}, fmt.Errorf("sampling: %w", err)
	}
	return m, nil
}
