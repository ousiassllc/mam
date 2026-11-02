package gentesttest

import (
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localbackends"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
)

// The local image models WithTemplateModels adds, each the bundled template as it is shipped: SamplingModel is SDXL (takes the steps, cfg,
// sampler and scheduler, 25 steps, cfg 7, dpmpp_2m, karras; CLIP reads the prompt weights), WeakWeightModel is FLUX.1 dev (a T5: the weights
// are applied but may be weak) and NoWeightModel is Qwen-Image (its encoder does not read the weights).
var (
	SamplingModel   = templateModel("sdxl")
	WeakWeightModel = templateModel("flux-dev")
	NoWeightModel   = templateModel("qwen-image")
)

func templateModel(name string) domain.ModelName {
	return domain.ModelName(localcfg.ID(localcfg.KindComfyUI, EndpointName, name))
}

var templateOf = map[domain.ModelName]string{SamplingModel: "txt2img-sdxl", WeakWeightModel: "txt2img-flux-dev", NoWeightModel: "txt2img-qwen-image"}

// WithTemplateModels adds SamplingModel, WeakWeightModel and NoWeightModel to the rig, with a fake backend, at the endpoint
// "bundled-comfyui" (a closed port: nothing is generated).
func WithTemplateModels() Option {
	return func(r *Rig, d *gentest.Deps) {
		r.Config.Endpoints = map[string]localcfg.Endpoint{EndpointName: {Kind: localcfg.KindComfyUI, BaseURL: "http://127.0.0.1:1"}}
		r.Config.LocalModels = map[string]localcfg.Model{}
		for m, id := range templateOf {
			tpl, ok := localcfg.TemplateByID(id)
			if !ok {
				panic("gentesttest: the template " + id + " is gone")
			}
			_, _, name, _ := localcfg.ParseID(string(m))
			r.Config.LocalModels[localcfg.Key(EndpointName, name)] = tpl.Model
			r.extra = append(r.extra, m)
		}
		d.BundledEndpoint = EndpointName
		localbackends.Publish(r.Config)
		r.T.Cleanup(func() { localbackends.Publish(modeladapter.DefaultConfig()) })
	}
}
