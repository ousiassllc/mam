package gentesttest

import (
	"encoding/json"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localbackends"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/poses"
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
	// the references are optional, as in the templates of the editing and img2img models: they run (as text to image) without one
	workflow := func(mode localcfg.ReferenceMode, refs int, lora string) *localcfg.Workflow {
		w := &localcfg.Workflow{ReferenceMode: mode, LoraModelNode: lora}
		for i := range refs {
			w.Assignments = append(w.Assignments, localcfg.Assignment{Param: localcfg.ParamReferenceImage, Node: "1", Input: "image", Index: i, Optional: true})
		}
		return w
	}
	edit := workflow(localcfg.ModeEdit, 3, "7")
	edit.LoraFamily = "qwen-image" // a LoRA made for another family of base model is warned about (#1119)
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

// SpeedModel is the local ComfyUI video model WithSpeedModel adds: a first-frame model with two speed modes, fast (the default) and standard
// (Wan 2.2 14B with the LightX2V LoRA, Issue #930). ImageSpeedModel is the image model it adds with it: three modes, standard (the default),
// lightning-8 and lightning-4 (Qwen-Image-Edit 2511 with the Lightning LoRAs, Issue #991).
var (
	SpeedModel      = domain.ModelName(localcfg.ID(localcfg.KindComfyUI, EndpointName, "wan"))
	ImageSpeedModel = domain.ModelName(localcfg.ID(localcfg.KindComfyUI, EndpointName, "lightning"))
)

// WithSpeedModel adds SpeedModel and ImageSpeedModel to the rig, with a fake backend, at the endpoint "bundled-comfyui" ("" is a closed port).
func WithSpeedModel() Option {
	return func(r *Rig, d *gentest.Deps) {
		w := &localcfg.Workflow{
			Assignments: []localcfg.Assignment{{Param: localcfg.ParamFirstFrame, Node: "1", Input: "image"}},
			SpeedModes:  []localcfg.SpeedMode{{ID: "fast", Label: "4 ステップ（速い）"}, {ID: "standard", Label: "通常（20 ステップ）"}},
		}
		image := &localcfg.Workflow{
			Graph:       json.RawMessage(`{"1": {"class_type": "KSampler", "inputs": {"steps": 20, "cfg": 4}}}`),
			Assignments: []localcfg.Assignment{{Param: localcfg.ParamSteps, Node: "1", Input: "steps"}, {Param: localcfg.ParamCFG, Node: "1", Input: "cfg"}},
			SpeedModes:  []localcfg.SpeedMode{{ID: "standard", Label: "通常（20 ステップ）"}, {ID: "lightning-8", Label: "8 ステップ（Lightning）"}, {ID: "lightning-4", Label: "4 ステップ（Lightning）"}},
		}
		r.Config.Endpoints = map[string]localcfg.Endpoint{EndpointName: {Kind: localcfg.KindComfyUI, BaseURL: "http://127.0.0.1:1"}}
		r.Config.LocalModels = map[string]localcfg.Model{
			localcfg.Key(EndpointName, "wan"):       {Media: localcfg.MediaVideo, Workflow: w, Video: &localcfg.VideoSettings{Durations: []float64{3, 5}, Resolutions: []string{"480p", "720p"}}},
			localcfg.Key(EndpointName, "lightning"): {Media: localcfg.MediaImage, Workflow: image},
		}
		r.extra = append(r.extra, SpeedModel, ImageSpeedModel)
		d.BundledEndpoint = EndpointName
		localbackends.Publish(r.Config)
		r.T.Cleanup(func() { localbackends.Publish(modeladapter.DefaultConfig()) })
	}
}

// WithPoseLibrary gives the Service a pose library (Deps.Poses; poses/posestest builds an installed one over a fake GitHub).
func WithPoseLibrary(lib *poses.Library) Option {
	return func(_ *Rig, d *gentest.Deps) { d.Poses = lib }
}

// The local ComfyUI models WithInputModels adds, one for each way a model takes input images (#1009): I2VModel needs a first frame, FLFModel a first and a
// last frame, T2VModel takes no image, ControlModel is an image model that cannot run without its reference (ControlNet).
var (
	I2VModel     = domain.ModelName(localcfg.ID(localcfg.KindComfyUI, EndpointName, "i2v"))
	FLFModel     = domain.ModelName(localcfg.ID(localcfg.KindComfyUI, EndpointName, "flf"))
	T2VModel     = domain.ModelName(localcfg.ID(localcfg.KindComfyUI, EndpointName, "t2v"))
	ControlModel = domain.ModelName(localcfg.ID(localcfg.KindComfyUI, EndpointName, "control"))
)

// WithInputModels adds I2VModel, FLFModel, T2VModel and ControlModel to the rig, with a fake backend, at the endpoint "bundled-comfyui".
func WithInputModels() Option {
	return func(r *Rig, d *gentest.Deps) {
		video := &localcfg.VideoSettings{Durations: []float64{5}, Resolutions: []string{"720p"}}
		frame := func(params ...localcfg.Param) *localcfg.Workflow {
			w := &localcfg.Workflow{}
			for i, p := range params {
				w.Assignments = append(w.Assignments, localcfg.Assignment{Param: p, Node: "1", Input: "in" + string(rune('a'+i))})
			}
			return w
		}
		control := &localcfg.Workflow{ReferenceMode: localcfg.ModeControlNet, Assignments: []localcfg.Assignment{{Param: localcfg.ParamReferenceImage, Node: "1", Input: "image"}}}
		r.Config.Endpoints = map[string]localcfg.Endpoint{EndpointName: {Kind: localcfg.KindComfyUI, BaseURL: "http://127.0.0.1:1"}}
		r.Config.LocalModels = map[string]localcfg.Model{
			localcfg.Key(EndpointName, "i2v"):     {Media: localcfg.MediaVideo, Workflow: frame(localcfg.ParamFirstFrame), Video: video},
			localcfg.Key(EndpointName, "flf"):     {Media: localcfg.MediaVideo, Workflow: frame(localcfg.ParamFirstFrame, localcfg.ParamLastFrame), Video: video},
			localcfg.Key(EndpointName, "t2v"):     {Media: localcfg.MediaVideo, Workflow: frame(), Video: video},
			localcfg.Key(EndpointName, "control"): {Media: localcfg.MediaImage, Workflow: control},
		}
		r.extra = append(r.extra, I2VModel, FLFModel, T2VModel, ControlModel)
		d.BundledEndpoint = EndpointName
		localbackends.Publish(r.Config)
		r.T.Cleanup(func() { localbackends.Publish(modeladapter.DefaultConfig()) })
	}
}
