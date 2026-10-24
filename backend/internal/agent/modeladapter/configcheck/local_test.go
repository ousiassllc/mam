package configcheck_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/configcheck"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

func withLocal(t *testing.T) modeladapter.Config {
	t.Helper()
	cfg := modeladapter.DefaultConfig()
	cfg.Endpoints = map[string]localcfg.Endpoint{
		"box": {Kind: localcfg.KindComfyUI, BaseURL: "http://100.101.102.103:8188"},
		"oa":  {Kind: localcfg.KindOpenAICompat, BaseURL: "http://localhost:1234"},
	}
	img, _ := localcfg.TemplateByID("txt2img-sdxl")
	vid, _ := localcfg.TemplateByID("txt2video-wan22-5b")
	cfg.LocalModels = map[string]localcfg.Model{
		"box/img": img.Model, "box/vid": vid.Model,
		"oa/flux": {Media: localcfg.MediaImage, Model: "flux-dev"},
	}
	return cfg
}

func TestLocalModelsAreValidAndRoutable(t *testing.T) {
	cfg := withLocal(t)
	cfg.Images[modeladapter.PurposePreview] = modeladapter.ImageRoute{
		Primary:  modeladapter.ImageTarget{Model: "comfyui:box/img"},
		Fallback: modeladapter.ImageTarget{Model: "openai-compat:oa/flux"},
	}
	cfg.Policies[domain.QualityFast] = modeladapter.PolicyRoute{
		Primary:  modeladapter.VideoTarget{Model: "comfyui:box/vid", Resolution: "480p"}, // no price: a local model is free
		Fallback: modeladapter.VideoTarget{Model: domain.ModelKling3, Resolution: "720p"}, Takes: 1,
	}
	require.NoError(t, configcheck.Validate(cfg))
	assert.Equal(t, modeladapter.Vendor("endpoint:box"), modeladapter.VendorOf("comfyui:box/img"))
	assert.Equal(t, modeladapter.Vendor("endpoint:oa"), modeladapter.VendorOf("openai-compat:oa/flux"))
	assert.Equal(t, modeladapter.Vendor("kling"), modeladapter.VendorOf(domain.ModelKling3))
	assert.Zero(t, cfg.VideoCost(cfg.Policies[domain.QualityFast].Primary, 5), "0 USD by default")
	assert.Equal(t, 1, cfg.VendorSettings()["endpoint:box"].MaxConcurrency, "one GPU, one job")
	assert.Equal(t, 3600, cfg.VendorSettings()["endpoint:box"].TimeoutSeconds, "a local video takes long")
}

func TestRoutesMustNameDefinedLocalModels(t *testing.T) {
	cfg := withLocal(t)
	cfg.Images[modeladapter.PurposePreview] = modeladapter.ImageRoute{
		Primary:  modeladapter.ImageTarget{Model: "comfyui:box/vid"},     // a video model in an image route
		Fallback: modeladapter.ImageTarget{Model: "comfyui:box/missing"}, // not defined
	}
	cfg.Policies[domain.QualityFast] = modeladapter.PolicyRoute{
		Primary:  modeladapter.VideoTarget{Model: "comfyui:box/vid", Resolution: "1080p"}, // not in its resolutions
		Fallback: modeladapter.VideoTarget{Model: "comfyui:box/img", Resolution: "720p"},  // an image model
		Takes:    1,
	}
	err := configcheck.Validate(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `images preview primary: "comfyui:box/vid" is not a configured image model`)
	assert.Contains(t, err.Error(), `images preview fallback: "comfyui:box/missing" is not a configured image model`)
	assert.Contains(t, err.Error(), `does not support "1080p"`)
	assert.Contains(t, err.Error(), `policies fast fallback: "comfyui:box/img" is not a configured video model`)
}

func TestInvalidEndpointsAndModelsAreReported(t *testing.T) {
	cfg := withLocal(t)
	cfg.Endpoints["Bad Name"] = localcfg.Endpoint{Kind: localcfg.KindComfyUI, BaseURL: "http://h"}
	cfg.Endpoints["lnk"] = localcfg.Endpoint{Kind: localcfg.KindComfyUI, BaseURL: "http://169.254.169.254"}
	cfg.LocalModels["ghost/m"] = localcfg.Model{Media: localcfg.MediaImage, Model: "x"}
	err := configcheck.Validate(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Bad Name")
	assert.Contains(t, err.Error(), "link-local")
	assert.Contains(t, err.Error(), `the endpoint "ghost" is not registered`)
}

func TestLoadReadsEndpointsAndModelsFromTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	require.NoError(t, os.WriteFile(path, []byte(`{
	  "endpoints": {"oa": {"kind": "openai-compat", "base_url": "http://192.168.1.9:8000", "max_concurrency": 2}},
	  "local_models": {"oa/flux": {"media": "image", "model": "flux-dev", "cost_per_image_usd": 0.01, "accepts_image_inputs": true}},
	  "images": {"new_reference": {"primary": {"model": "openai-compat:oa/flux"}, "fallback": {"model": "imagen-4"}}}
	}`), 0o600))
	cfg, err := configcheck.Load(path)
	require.NoError(t, err)
	assert.Equal(t, 2, cfg.Endpoints["oa"].Concurrency())
	assert.True(t, cfg.AcceptsImages("openai-compat:oa/flux"))
	assert.False(t, cfg.AcceptsImages(domain.ModelImagen4))
	assert.InDelta(t, 0.01, cfg.ImageCost("openai-compat:oa/flux"), 1e-9)

	for name, bad := range map[string]string{
		"unknown field of an endpoint": `{"endpoints":{"oa":{"kind":"openai-compat","base_url":"http://h","typo":1}}}`,
		"unknown field of a model":     `{"endpoints":{"oa":{"kind":"openai-compat","base_url":"http://h"}},"local_models":{"oa/m":{"media":"image","model":"x","typo":1}}}`,
		"unknown top-level key":        `{"endpoint":{}}`,
	} {
		require.NoError(t, os.WriteFile(path, []byte(bad), 0o600))
		_, err := configcheck.Load(path)
		assert.Error(t, err, name)
	}
}
