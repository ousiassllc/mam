package settingstest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	mcpgentest "github.com/ousiassllc/mam/backend/internal/mcp/tools/gentest"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/traits"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
)

// catalogInfo is what the model manager answers for the models of the bundled endpoint (mmview.InfoOf): found by the workflow name in the embedded catalog.
func catalogInfo(id string) (modelfilter.CatalogInfo, bool) {
	for _, m := range catalog.Default().Models {
		for _, w := range m.Workflows {
			if gentesttest.InstalledID(w.Name) == id {
				return modelfilter.CatalogInfo{Adult: m.Adult(), Traits: m.Traits, VRAMMinGB: m.VRAM.MinGB}, true
			}
		}
	}
	return modelfilter.CatalogInfo{}, false
}

func filterRig(t *testing.T) *rig {
	t.Helper()
	return newRig(t,
		gentesttest.WithoutBackend(domain.ModelNanoBananaLite),
		gentesttest.WithInstalled(map[string]string{
			"wan22-i2v-14b": "img2video-wan22-14b", "wan22-remix-nsfw-t2v": "txt2video-wan22-14b-remix", "flux1-schnell": "txt2img-flux-schnell",
			"qwen-image-edit-2511": "txt2img-qwen-image-edit-2511", "sdxl-base": "txt2img-sdxl",
		}),
		gentesttest.WithCatalogInfo(catalogInfo))
}

func listIDs(t *testing.T, r *rig, args map[string]any) ([]string, mcpgentest.ListModelsOutput) {
	t.Helper()
	var out mcpgentest.ListModelsOutput
	r.CallOK("list_generation_models", args, &out)
	ids := []string{}
	for _, m := range out.Models {
		ids = append(ids, m.ID)
	}
	return ids, out
}

func TestListModelsFiltersByInputsAdultUncensoredAndParameters(t *testing.T) {
	r := filterRig(t)
	wan, remix, qwenEdit := gentesttest.InstalledID("wan22-i2v-14b"), gentesttest.InstalledID("wan22-remix-nsfw-t2v"), gentesttest.InstalledID("qwen-image-edit-2511")

	ids, all := listIDs(t, r, map[string]any{})
	by := map[string]modelcatalog.Info{}
	for _, m := range all.Models {
		by[m.ID] = m
	}
	require.Contains(t, by, wan)
	assert.Equal(t, "local", by[wan].Origin)
	assert.Equal(t, "local", by[wan].Provider)
	assert.Contains(t, by[wan].Inputs, traits.FirstFrame, "the inputs follow the workflow's first frame")
	assert.Contains(t, by[wan].Inputs, traits.I2V)
	assert.NotContains(t, by[wan].Inputs, traits.T2V, "a Wan 2.2 I2V workflow cannot run without a frame")
	assert.Equal(t, []traits.Input{traits.T2V}, by[remix].Inputs)
	assert.Contains(t, by[qwenEdit].Inputs, traits.ReferenceFace)
	require.NotNil(t, by[wan].Catalog)
	assert.InDelta(t, 14, by[wan].Catalog.Traits.ParamsB, 1e-9)
	assert.False(t, by[wan].Catalog.Adult)
	assert.True(t, by[remix].Catalog.Adult, "the adult flag comes from the catalog entry")
	assert.Nil(t, by["flux-2"].Catalog, "a cloud model is not in the catalog")
	assert.Equal(t, "api", by["flux-2"].Origin)
	assert.Contains(t, by["flux-2"].Inputs, traits.T2I)
	assert.Contains(t, by["runway-gen-4.5"].Inputs, traits.T2V, "a cloud video model can leave the frame out")
	assert.Contains(t, by["runway-gen-4.5"].Inputs, traits.FirstFrame)
	assert.Contains(t, ids, "flux-2", "no argument narrows nothing")

	only := func(args map[string]any) []string {
		got, _ := listIDs(t, r, args)
		return got
	}
	assert.Equal(t, []string{wan, remix}, only(map[string]any{"media": "video", "origins": []string{"local"}}))
	assert.Equal(t, []string{remix}, only(map[string]any{"media": "video", "adult": "adult_only", "origins": []string{"local"}}))
	assert.Equal(t, []string{remix}, only(map[string]any{"uncensored": "uncensored_only"}))
	assert.Equal(t, []string{wan}, only(map[string]any{"media": "video", "origins": []string{"local"}, "adult": "general_only", "params": []string{"large"}}), "14B is large")
	assert.Equal(t, []string{remix}, only(map[string]any{"inputs": []string{"t2v"}, "origins": []string{"local"}}))
	assert.Equal(t, []string{qwenEdit}, only(map[string]any{"kinds": []string{"image_edit"}, "origins": []string{"local"}}), "an editing image model is image and image_edit")
	assert.Equal(t, []string{qwenEdit}, only(map[string]any{"inputs": []string{"reference_face", "edit"}}))
	assert.Equal(t, []string{wan, remix}, only(map[string]any{"vram_fit": true, "media": "video"}), "VRAM 16GB: the two 14B at 16 GB; the cloud models have no guideline")
	assert.Equal(t, []string{wan}, only(map[string]any{"q": "WAN22-i2v"}))
	assert.Contains(t, r.CallError("list_generation_models", map[string]any{"adult": "yes"}), "validation")
	assert.Contains(t, r.CallError("list_generation_models", map[string]any{"params": []string{"huge"}}), "validation")
}

// #1013: every model says whether it can be chosen now, why not, and where to fix it; a picker's "選べるものだけ" is available_only.
func TestListModelsSaysWhyAModelCannotBeChosenAndWhereToFixIt(t *testing.T) {
	r := filterRig(t)
	_, out := listIDs(t, r, map[string]any{})
	by := map[string]modelcatalog.Info{}
	for _, m := range out.Models {
		by[m.ID] = m
	}
	lite := by["nano-banana-lite"]
	assert.False(t, lite.Available)
	assert.Equal(t, "no_backend", lite.Unavailable)
	assert.Contains(t, lite.UnavailableMessage, "API キー")
	assert.Equal(t, "/settings/api-keys", lite.SettingsRoute, "an API key is registered on the settings screen")
	assert.True(t, by["flux-2"].Available)
	assert.Empty(t, by["flux-2"].SettingsRoute)
	assert.Equal(t, modelfilter.Unusable(modelfilter.ReasonNoKey, lite.UnavailableMessage, "/settings/api-keys"), lite.Availability())
	assert.True(t, by["flux-2"].Availability().Available)

	ids, avail := listIDs(t, r, map[string]any{"available_only": true})
	assert.NotContains(t, ids, "nano-banana-lite", "what cannot be chosen is hidden until asked for")
	assert.Contains(t, ids, "flux-2")
	assert.Less(t, len(avail.Models), len(out.Models))
	for _, m := range avail.Models {
		assert.True(t, m.Available, m.ID)
	}
}

func TestEveryUnavailableModelHasARouteExceptAFailureThatFixesItself(t *testing.T) {
	r := filterRig(t)
	_, out := listIDs(t, r, map[string]any{})
	for _, m := range out.Models {
		if m.Available {
			assert.Empty(t, m.Unavailable, m.ID)
			continue
		}
		assert.NotEmpty(t, m.UnavailableMessage, m.ID)
		if m.Unavailable != "circuit_open" {
			assert.NotEmpty(t, m.SettingsRoute, m.ID+": the reason says where to fix it")
		}
	}
}
