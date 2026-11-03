package choice_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/choice"
)

func valid() []choice.Choice {
	return []choice.Choice{
		{
			ID: "small", Name: "Small", Description: "既定", Default: true, Kind: "tts", Languages: []string{"ja"}, License: "mit",
			Cautions: []string{"本人の同意なく声をコピーしない"}, VRAMMinGB: 6, VRAMRecommendedGB: 8, VRAMLimits: "長い文で足りなくなる", VRAMNote: "mam の推定", VRAMEstimate: true,
		},
		{
			ID: "large", Name: "Large", Description: "大きい", Kind: "tts", Languages: []string{"ja"}, License: "gemma",
			Consent:  &choice.Consent{Version: "v1", Text: "規約に同意します"},
			Cautions: []string{"禁止用途ポリシーに従う"}, VRAMMinGB: 10, VRAMNote: "mam の推定", Env: []string{"IRODORI_MODEL_PRECISION=bf16"},
		},
	}
}

var (
	licenses = map[string]bool{"mit": true, "gemma": true}
	used     = map[string]bool{"": true, "small": true, "large": true}
)

func TestCheck_AValidSetPasses(t *testing.T) {
	assert.Empty(t, choice.Check("set", valid(), licenses, used))
	assert.Empty(t, choice.Check("set", nil, licenses, map[string]bool{"": true}), "a runtime without choices has nothing to check")
}

func TestCheck_RefusesWhatTheScreenCouldNotShowHonestly(t *testing.T) {
	for name, tc := range map[string]struct {
		edit func(cs []choice.Choice) []choice.Choice
		want string
	}{
		"a duplicate id":              {func(cs []choice.Choice) []choice.Choice { cs[1].ID = "small"; return cs }, "used once"},
		"an id that is not a slug":    {func(cs []choice.Choice) []choice.Choice { cs[1].ID = "Large Model"; return cs }, "slug"},
		"no description":              {func(cs []choice.Choice) []choice.Choice { cs[0].Description = " "; return cs }, "description"},
		"no kind":                     {func(cs []choice.Choice) []choice.Choice { cs[0].Kind = ""; return cs }, "kind"},
		"no languages":                {func(cs []choice.Choice) []choice.Choice { cs[0].Languages = nil; return cs }, "languages"},
		"a language that is no code":  {func(cs []choice.Choice) []choice.Choice { cs[0].Languages = []string{"Japanese"}; return cs }, "languages"},
		"a license that is not there": {func(cs []choice.Choice) []choice.Choice { cs[0].License = "nope"; return cs }, "license"},
		"no cautions":                 {func(cs []choice.Choice) []choice.Choice { cs[0].Cautions = nil; return cs }, "cautions"},
		"no minimum":                  {func(cs []choice.Choice) []choice.Choice { cs[0].VRAMMinGB = 0; return cs }, "vram"},
		"no source of the figure":     {func(cs []choice.Choice) []choice.Choice { cs[0].VRAMNote = ""; return cs }, "vram"},
		"a recommendation without what a smaller GPU gives up": {func(cs []choice.Choice) []choice.Choice { cs[0].VRAMLimits = ""; return cs }, "vram_limits"},
		"a consent without its text":                           {func(cs []choice.Choice) []choice.Choice { cs[1].Consent.Text = ""; return cs }, "consent"},
		"a consent without a version":                          {func(cs []choice.Choice) []choice.Choice { cs[1].Consent.Version = ""; return cs }, "consent"},
		"an env that is not NAME=value":                        {func(cs []choice.Choice) []choice.Choice { cs[1].Env = []string{"bf16"}; return cs }, "NAME=value"},
		"a value that adds an argument": {func(cs []choice.Choice) []choice.Choice {
			cs[1].Values = map[string]string{"ngl": "all --x"}
			return cs
		}, "plain word"},
		"a value named like a built-in placeholder": {func(cs []choice.Choice) []choice.Choice { cs[1].Values = map[string]string{"port": "1"}; return cs }, "plain word"},
		"a value whose name is no placeholder":      {func(cs []choice.Choice) []choice.Choice { cs[1].Values = map[string]string{"Ctx-Size": "1"}; return cs }, "plain word"},
		"no default":                                {func(cs []choice.Choice) []choice.Choice { cs[0].Default = false; return cs }, "exactly one"},
		"two defaults":                              {func(cs []choice.Choice) []choice.Choice { cs[1].Default = true; return cs }, "exactly one"},
	} {
		t.Run(name, func(t *testing.T) {
			errs := choice.Check("set", tc.edit(valid()), licenses, used)

			require.NotEmpty(t, errs)
			assert.Contains(t, fmt.Sprint(errs), tc.want)
		})
	}
}

func TestCheck_AcceptsTheValuesOfALlamaServerModel(t *testing.T) {
	cs := valid()
	cs[1].Values = map[string]string{"ctx_size": "16384", "ngl": "auto"}
	cs[1].Uncensored = true
	assert.Empty(t, choice.Check("set", cs, licenses, used))
}

func TestCheck_EveryChoiceHasAssetsAndEveryAssetsChoiceExists(t *testing.T) {
	assert.Contains(t, fmt.Sprint(choice.Check("set", valid(), licenses, map[string]bool{"": true, "small": true})), "no asset belongs")
	assert.Contains(t, fmt.Sprint(choice.Check("set", valid(), licenses, map[string]bool{"": true, "small": true, "large": true, "xl": true})), `"xl", which does not exist`)
}
