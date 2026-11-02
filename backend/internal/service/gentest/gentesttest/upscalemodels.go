package gentesttest

import (
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localbackends"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
)

// The upscale workflows WithUpscaleModels adds, each the bundled template as it is shipped: UpscaleModel enlarges with the upscale model
// alone, FinishModel finishes the enlarged picture with SDXL (it takes the prompt and the seed of its source).
var (
	UpscaleModel = templateModel("upscale-nomos-uni-span")
	FinishModel  = templateModel("upscale-2pass-sdxl-span")
)

var upscaleTemplateOf = map[domain.ModelName]string{UpscaleModel: "upscale-nomos-uni-span", FinishModel: "upscale-2pass-sdxl-span"}

// WithUpscaleModels adds UpscaleModel and FinishModel to the rig, with a fake backend (that answers the one-pixel PNG whatever the
// scale), at the endpoint "bundled-comfyui" (a closed port). The other models of the rig stay.
func WithUpscaleModels() Option {
	return func(r *Rig, d *gentest.Deps) {
		if r.Config.Endpoints == nil {
			r.Config.Endpoints = map[string]localcfg.Endpoint{}
		}
		if r.Config.LocalModels == nil {
			r.Config.LocalModels = map[string]localcfg.Model{}
		}
		r.Config.Endpoints[EndpointName] = localcfg.Endpoint{Kind: localcfg.KindComfyUI, BaseURL: "http://127.0.0.1:1"}
		for m, id := range upscaleTemplateOf {
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
