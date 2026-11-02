package gentesttest

import (
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localbackends"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
)

// The local ComfyUI image models WithReferenceModels adds (ids of the bundled engine's models).
var (
	// EditModel takes up to 3 references with roles, a LoRA and a negative prompt field (an editing model); Img2ImgModel takes one reference as a whole
	// picture and no LoRA; PlainModel takes no reference.
	EditModel    = domain.ModelName(localcfg.ID(localcfg.KindComfyUI, EndpointName, "edit"))
	Img2ImgModel = domain.ModelName(localcfg.ID(localcfg.KindComfyUI, EndpointName, "i2i"))
	PlainModel   = domain.ModelName(localcfg.ID(localcfg.KindComfyUI, EndpointName, "plain"))
)

// EndpointName is the name of the bundled ComfyUI endpoint of the rig.
const EndpointName = "bundled-comfyui"

// WithReferenceModels adds EditModel, Img2ImgModel and PlainModel to the rig, with a fake backend, at the endpoint "bundled-comfyui"
// whose address is baseURL (the stub of the LoRA list; "" is a closed port). They are registered process-wide until the test ends.
func WithReferenceModels(baseURL string) Option {
	if baseURL == "" {
		baseURL = "http://127.0.0.1:1"
	}
	workflow := func(mode localcfg.ReferenceMode, refs int, lora string) *localcfg.Workflow {
		w := &localcfg.Workflow{ReferenceMode: mode, LoraModelNode: lora}
		for i := range refs {
			w.Assignments = append(w.Assignments, localcfg.Assignment{Param: localcfg.ParamReferenceImage, Node: "1", Input: "image", Index: i})
		}
		return w
	}
	edit := workflow(localcfg.ModeEdit, 3, "7")
	edit.Assignments = append(edit.Assignments, localcfg.Assignment{Param: localcfg.ParamNegativePrompt, Node: "2", Input: "text"})
	return func(r *Rig, d *gentest.Deps) {
		r.Config.Endpoints = map[string]localcfg.Endpoint{EndpointName: {Kind: localcfg.KindComfyUI, BaseURL: baseURL}}
		r.Config.LocalModels = map[string]localcfg.Model{
			localcfg.Key(EndpointName, "edit"):  {Media: localcfg.MediaImage, Workflow: edit},
			localcfg.Key(EndpointName, "i2i"):   {Media: localcfg.MediaImage, Workflow: workflow(localcfg.ModeImg2Img, 1, "")},
			localcfg.Key(EndpointName, "plain"): {Media: localcfg.MediaImage, Workflow: workflow("", 0, "")},
		}
		r.extra = append(r.extra, EditModel, Img2ImgModel, PlainModel)
		d.BundledEndpoint = EndpointName
		localbackends.Publish(r.Config)
		r.T.Cleanup(func() { localbackends.Publish(modeladapter.DefaultConfig()) })
	}
}
