package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
)

// The production wiring of the reference image support (#933): a catalog model installed before its template learned reference
// images is saved in models.json as the old text-to-image copy; starting mam replaces it with the template's, which takes a
// reference image, and keeps the price the user set.
func TestBuildServices_AnInstalledModelOfBeforeGetsTheReferenceImageSupport(t *testing.T) {
	cfg := testConfig(t)
	var wf catalog.Workflow
	for _, m := range catalog.Default().Models {
		for _, w := range m.Workflows {
			if w.Template == "txt2img-chroma" {
				wf = w
			}
		}
	}
	require.NotEmpty(t, wf.Name, "the catalog has a model made from the Chroma template")
	tpl, ok := localcfg.TemplateByID(wf.Template)
	require.True(t, ok)
	old := tpl.Model
	w := *old.Workflow
	w.ReferenceMode, w.NoReference = "", nil // the copy of before the template took a reference
	old.Workflow, old.CostPerImage = &w, 0.07

	file, err := json.Marshal(map[string]any{
		"endpoints":    map[string]any{"bundled-comfyui": map[string]any{"kind": "comfyui", "base_url": "http://127.0.0.1:8188"}},
		"local_models": map[string]any{"bundled-comfyui/" + wf.Name: old},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(cfg.DataDir, "models.json"), file, 0o600))

	services, _, cleanup, err := buildServices(context.Background(), cfg, slog.New(slog.DiscardHandler), testChatGPT(cfg), testFFmpegDL(cfg))
	require.NoError(t, err)
	t.Cleanup(cleanup)

	view, err := services.LocalModels.View()
	require.NoError(t, err)
	var got *localcfg.Model
	for _, m := range view.Models {
		if m.Endpoint == "bundled-comfyui" && m.Name == wf.Name {
			got = &m.Model
		}
	}
	require.NotNil(t, got)
	assert.Equal(t, localcfg.ModeImg2Img, got.Workflow.ReferenceMode)
	assert.True(t, got.ImageInputs(localcfg.KindComfyUI))
	assert.InDelta(t, 0.07, got.CostPerImage, 1e-9, "the price the user set is kept")
}
