package gentesttest

import (
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localbackends"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/exposed"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
)

// ExposedModel is SamplingModel's workflow (SDXL) with three exposed inputs (Issue #977): the denoise of its KSampler (a float, 0 to 1, the graph holds 1),
// the batch size of its empty latent (an int, 1 to 4, the graph holds 1) and the prefix of the file name its SaveImage writes (a string). Its
// steps, cfg, sampler and scheduler are assigned, so they are the usual detailed settings.
var ExposedModel = templateModel("sdxl-exposed")

// The keys of the exposed inputs of ExposedModel.
const (
	ExposedDenoise = "3.denoise"
	ExposedBatch   = "5.batch_size"
	ExposedPrefix  = "9.filename_prefix"
)

// WithExposedModel adds ExposedModel to the rig, at the endpoint "bundled-comfyui" (see WithTemplateModels, which it also needs for the endpoint).
func WithExposedModel() Option {
	return func(r *Rig, d *gentest.Deps) {
		tpl, ok := localcfg.TemplateByID("txt2img-sdxl")
		if !ok {
			panic("gentesttest: the template txt2img-sdxl is gone")
		}
		zero, one, four := 0.0, 1.0, 4.0
		m := tpl.Model
		w := *m.Workflow
		w.ExposedInputs = []exposed.Input{
			{Node: "3", Input: "denoise", Type: exposed.Float, Label: "ノイズの量", Min: &zero, Max: &one},
			{Node: "5", Input: "batch_size", Type: exposed.Int, Label: "同時に作る枚数", Min: &one, Max: &four},
			{Node: "9", Input: "filename_prefix", Type: exposed.String, Label: "ファイル名の先頭"},
		}
		m.Workflow = &w
		if r.Config.Endpoints == nil {
			r.Config.Endpoints = map[string]localcfg.Endpoint{EndpointName: {Kind: localcfg.KindComfyUI, BaseURL: "http://127.0.0.1:1"}}
		}
		if r.Config.LocalModels == nil {
			r.Config.LocalModels = map[string]localcfg.Model{}
		}
		_, _, name, _ := localcfg.ParseID(string(ExposedModel))
		r.Config.LocalModels[localcfg.Key(EndpointName, name)] = m
		r.extra = append(r.extra, ExposedModel)
		d.BundledEndpoint = EndpointName
		localbackends.Publish(r.Config)
		r.T.Cleanup(func() { localbackends.Publish(modeladapter.DefaultConfig()) })
	}
}
