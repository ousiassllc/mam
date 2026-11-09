// Package routetest is the observed configuration of Issue #1113 for the tests of modelroute and of the generation that uses it: a machine with no API key
// registered, the ChatGPT subscription signed in, and a ComfyUI of its own with image and video models.
package routetest

import (
	"testing"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/chatgptid"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/modelreg"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
)

// The models of the observed configuration (Issue #1113): ChatGPT subscription image models, a local ComfyUI image model that takes reference images, one that
// cannot make a picture without one, and a local image-to-video workflow.
const (
	LocalImage  domain.ModelName = "comfyui:bundled-comfyui/flux-t2i"
	LocalEdit   domain.ModelName = "comfyui:bundled-comfyui/qwen-edit"
	LocalVideo  domain.ModelName = "comfyui:bundled-comfyui/wan22-i2v-14b"
	LocalVideo2 domain.ModelName = "comfyui:bundled-comfyui/ltx-t2v"
	LocalText   domain.ModelName = "comfyui:bundled-comfyui/sd-t2i" // makes a picture from text alone: it does not take reference images
)

// LocalConfig is DefaultConfig with the bundled ComfyUI endpoint and four models defined on it.
func LocalConfig() modeladapter.Config {
	cfg := modeladapter.DefaultConfig()
	cfg.Endpoints = map[string]localcfg.Endpoint{"bundled-comfyui": {Kind: localcfg.KindComfyUI, BaseURL: "http://127.0.0.1:8188"}}
	cfg.LocalModels = map[string]localcfg.Model{
		"bundled-comfyui/flux-t2i":  {Media: localcfg.MediaImage, Workflow: &localcfg.Workflow{Assignments: []localcfg.Assignment{{Param: localcfg.ParamReferenceImage, Optional: true}}}},
		"bundled-comfyui/sd-t2i":    {Media: localcfg.MediaImage, Workflow: &localcfg.Workflow{}},
		"bundled-comfyui/qwen-edit": {Media: localcfg.MediaImage, Workflow: &localcfg.Workflow{Assignments: []localcfg.Assignment{{Param: localcfg.ParamReferenceImage}}}},
		"bundled-comfyui/wan22-i2v-14b": {
			Media: localcfg.MediaVideo, Workflow: &localcfg.Workflow{Assignments: []localcfg.Assignment{{Param: localcfg.ParamFirstFrame}}},
			Video: &localcfg.VideoSettings{Durations: []float64{5}, Resolutions: []string{"480p", "720p"}},
		},
		"bundled-comfyui/ltx-t2v": {
			Media: localcfg.MediaVideo, Workflow: &localcfg.Workflow{},
			Video: &localcfg.VideoSettings{Durations: []float64{4, 8}, Resolutions: []string{"720p"}},
		},
	}
	return cfg
}

// Publish makes the models known to domain.ImageModels / VideoModels and to modeladapter.Capabilities for the test (they are process-wide).
func Publish(t *testing.T) {
	t.Helper()
	cfg := LocalConfig()
	modeladapter.PublishLocal(cfg.Endpoints, cfg.LocalModels, cfg.Workflows)
	t.Cleanup(func() { modeladapter.PublishLocal(nil, nil, nil) })
	modelreg.Set(
		[]string{string(chatgptid.Flare), string(chatgptid.Sunburst), string(chatgptid.Model), string(LocalImage), string(LocalEdit), string(LocalText)},
		[]string{string(LocalVideo), string(LocalVideo2)},
	)
	t.Cleanup(func() { modelreg.Set(nil, nil) })
}

// NoAPIKey is the judgement of the observed machine: every API model lacks its key, the subscription and the local models are fine.
func NoAPIKey(unusable ...domain.ModelName) func(domain.ModelName) modelfilter.Availability {
	return func(m domain.ModelName) modelfilter.Availability {
		for _, u := range unusable {
			if u == m {
				return modelfilter.Unusable(modelfilter.ReasonNotInstalled, "導入されていません", "/model-manager/image-video")
			}
		}
		if modelreg.RouteOf(string(m)) == modelreg.RouteAPI {
			return modelfilter.Unusable(modelfilter.ReasonNoKey, "API キーが登録されていません（設定の「API キー」）", "/settings/api-keys")
		}
		return modelfilter.Usable()
	}
}
