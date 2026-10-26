package hfplan

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

// SDXL templates an all-in-one SDXL checkpoint gets a model from (text to image, reference to image).
const (
	TemplateTxt2Img = "txt2img-sdxl"
	TemplateImg2Img = "img2img-sdxl"
)

// WithCheckpoint is the model of the template with the checkpoint file of its CheckpointLoaderSimple node replaced by name.
// The template must have exactly one such node. The template is not changed.
func WithCheckpoint(tpl localcfg.Template, name string) (localcfg.Model, error) {
	m := tpl.Model
	if m.Workflow == nil {
		return localcfg.Model{}, errors.New("hfplan: the template has no workflow")
	}
	g, err := localcfg.ParseGraph(m.Workflow.Graph)
	if err != nil {
		return localcfg.Model{}, err
	}
	var loader string
	for id, n := range g {
		if n.ClassType != "CheckpointLoaderSimple" {
			continue
		}
		if loader != "" {
			return localcfg.Model{}, fmt.Errorf("hfplan: template %s has more than one checkpoint loader", tpl.ID)
		}
		loader = id
	}
	if loader == "" {
		return localcfg.Model{}, fmt.Errorf("hfplan: template %s has no checkpoint loader", tpl.ID)
	}
	value, err := json.Marshal(name)
	if err != nil {
		return localcfg.Model{}, err
	}
	n := g[loader]
	inputs := make(map[string]json.RawMessage, len(n.Inputs))
	for k, v := range n.Inputs {
		inputs[k] = v
	}
	inputs["ckpt_name"] = value
	n.Inputs = inputs
	g[loader] = n
	graph, err := json.Marshal(g)
	if err != nil {
		return localcfg.Model{}, err
	}
	w := *m.Workflow
	w.Graph = graph
	m.Workflow = &w
	return m, nil
}
