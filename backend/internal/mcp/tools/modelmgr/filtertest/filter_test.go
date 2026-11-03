package filtertest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/tools/modelmgr"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/traits"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

func list(t *testing.T, env interface {
	CallOK(string, any, any)
}, args map[string]any) ([]string, modelmgr.ListCatalogOutput) {
	t.Helper()
	var cat modelmgr.ListCatalogOutput
	env.CallOK("list_model_catalog", args, &cat)
	ids := []string{}
	for _, m := range cat.Models {
		ids = append(ids, m.ID)
	}
	return ids, cat
}

// #1011: the entries carry the structured traits and the inputs derived from the workflows; the arguments are the filter panel's conditions.
func TestListModelCatalog_CarriesTheTraitsAndTheInputsOfEachModel(t *testing.T) {
	env, rig := newEnv(t)
	rig.InstallEngine()
	_, cat := list(t, env, map[string]any{})
	by := map[string]int{}
	for i, m := range cat.Models {
		by[m.ID] = i
	}
	a, b := cat.Models[by["m-a"]], cat.Models[by["m-b"]]
	assert.InDelta(t, 2.6, a.Traits.ParamsB, 1e-9)
	assert.Equal(t, "2.6B（UNet）", a.Traits.ParamsLabel)
	assert.False(t, a.Traits.Uncensored)
	assert.Equal(t, []traits.Input{traits.T2I, traits.I2I, traits.ReferenceWhole}, a.Inputs, "text-to-image and the img2img workflow of the model")
	assert.Equal(t, []traits.Input{traits.T2V}, b.Inputs, "the video workflow has no first frame")
	assert.False(t, b.Audio)
	assert.False(t, a.Available, "not installed: not available yet")
	assert.Equal(t, "not_installed", string(a.Reason))
	assert.Equal(t, "/model-manager", a.Route)
}

func TestListModelCatalog_FiltersByTheConditionsOfTheFilterPanel(t *testing.T) {
	env, rig := newEnv(t)
	rig.InstallEngine()
	for name, tc := range map[string]struct {
		args map[string]any
		want []string
	}{
		"nothing":                         {map[string]any{}, []string{"m-a", "m-b", "m-c"}},
		"video":                           {map[string]any{"kinds": []string{"video"}}, []string{"m-b"}},
		"image":                           {map[string]any{"kinds": []string{"image"}}, []string{"m-a", "m-c"}},
		"t2v":                             {map[string]any{"inputs": []string{"t2v"}}, []string{"m-b"}},
		"i2i and t2i":                     {map[string]any{"inputs": []string{"t2i", "i2i"}}, []string{"m-a"}},
		"params: small is up to 5B":       {map[string]any{"params": []string{"small"}}, []string{"m-a", "m-b"}},
		"params: medium":                  {map[string]any{"params": []string{"medium"}}, []string{"m-c"}},
		"params: large is from 14B":       {map[string]any{"params": []string{"large"}}, []string{}},
		"vram_fit":                        {map[string]any{"vram_fit": true}, []string{"m-a", "m-b", "m-c"}},
		"general only":                    {map[string]any{"adult": "general_only"}, []string{"m-a", "m-b", "m-c"}},
		"commercial ng":                   {map[string]any{"commercial": "ng"}, []string{"m-c"}},
		"commercial ok":                   {map[string]any{"commercial": "ok"}, []string{"m-a", "m-b"}},
		"not installed":                   {map[string]any{"installed": "not_installed_only"}, []string{"m-a", "m-b", "m-c"}},
		"installed":                       {map[string]any{"installed": "installed_only"}, []string{}},
		"available only":                  {map[string]any{"available_only": true}, []string{}},
		"fast":                            {map[string]any{"speeds": []string{"fast"}}, []string{"m-c"}},
		"search by name":                  {map[string]any{"q": "b"}, []string{"m-b"}},
		"cloud only: none of the catalog": {map[string]any{"origins": []string{"api"}}, []string{}},
		"combined: image and small":       {map[string]any{"kinds": []string{"image"}, "params": []string{"small"}}, []string{"m-a"}},
	} {
		got, _ := list(t, env, tc.args)
		assert.Equal(t, tc.want, got, name)
	}
	assert.Contains(t, env.CallError("list_model_catalog", map[string]any{"adult": "maybe"}), "validation")
	assert.Contains(t, env.CallError("list_model_catalog", map[string]any{"inputs": []string{"t2x"}}), "validation")
	assert.Contains(t, env.CallError("list_model_catalog", map[string]any{"commercial": "yes"}), "validation")
}

// The adult filters read the content rating and the uncensored filter reads the flag: both apply only after the age confirmation shows the adult model at all.
func TestListModelCatalog_AdultAndUncensoredFiltersAfterTheAgeConfirmation(t *testing.T) {
	env, rig := newEnv(t)
	rig.InstallEngine()
	confirmAdult(t, env)
	all, _ := list(t, env, map[string]any{})
	require.Contains(t, all, "m-d")
	got, _ := list(t, env, map[string]any{"adult": "adult_only"})
	assert.Equal(t, []string{"m-d"}, got)
	got, _ = list(t, env, map[string]any{"uncensored": "uncensored_only"})
	assert.Equal(t, []string{"m-d"}, got)
	got, _ = list(t, env, map[string]any{"uncensored": "censored_only"})
	assert.NotContains(t, got, "m-d")
	got, _ = list(t, env, map[string]any{"adult": "adult_only", "params": []string{"large"}})
	assert.Equal(t, []string{"m-d"}, got, "14B exactly is large")
	got, _ = list(t, env, map[string]any{"adult": "general_only"})
	assert.NotContains(t, got, "m-d")
	got, _ = list(t, env, map[string]any{"q": "nsfwsks"})
	assert.Equal(t, []string{"m-d"}, got, "the trigger words are searched like tags")
}

func confirmAdult(t *testing.T, env interface {
	CallOK(string, any, any)
}) {
	t.Helper()
	var st modelmanager.AdultContentStatus
	env.CallOK("set_adult_content", map[string]any{"enabled": true, "confirm_adult": true, "notice_version": modelmanager.AdultNoticeVersion}, &st)
	require.True(t, st.Enabled)
}
