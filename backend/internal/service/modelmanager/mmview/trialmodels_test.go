package mmview_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmview"
)

// An upscale or a video enhancement workflow is no model to give a prompt to: the trial generation does not offer it (the generation test's
// "make it larger / smoother" runs it), while a model that makes pictures from a prompt stays.
func TestTrialModels_LeaveOutTheEnhancementWorkflows(t *testing.T) {
	rig := mmtest.New(t)
	cfg, backends := rig.Adapter.Snapshot()
	models := map[string]localcfg.Model{}
	for k, m := range cfg.LocalModels {
		models[k] = m
	}
	for _, id := range []string{"video-upscale-seedvr2-3b", "video-interpolate-rife-v4.25", "upscale-nomos-uni-span", "txt2img-sdxl"} {
		tpl, ok := localcfg.TemplateByID(id)
		require.True(t, ok, id)
		models[localcfg.Key(modelmanager.BundledEndpoint, id)] = tpl.Model
	}
	cfg.LocalModels = models
	cfg.Endpoints = map[string]localcfg.Endpoint{modelmanager.BundledEndpoint: {Kind: localcfg.KindComfyUI, BaseURL: "http://127.0.0.1:1"}}
	rig.Adapter.Reload(cfg, backends)

	var got []string
	for _, m := range mmview.TrialModels(rig.Svc) {
		got = append(got, m.Name)
	}

	assert.Contains(t, got, "txt2img-sdxl", "a model that takes a prompt stays")
	assert.NotContains(t, got, "video-upscale-seedvr2-3b")
	assert.NotContains(t, got, "video-interpolate-rife-v4.25")
	assert.NotContains(t, got, "upscale-nomos-uni-span")
}
