package settingsviewtest_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/promptweight"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/settingsview"
)

func model(label string, l promptweight.Level) modelcatalog.Info {
	return modelcatalog.Info{ID: "comfyui:bundled-comfyui/" + label, Label: label, PromptWeight: l}
}

// #1023: the four ways a model reads weights are said in a few short words, and a model that is not known is only "未確認" (no long sentence).
func TestWeightTitleIsShortAndTheUnknownIsOneWord(t *testing.T) {
	assert.Equal(t, "効きます", settingsview.WeightTitle(promptweight.Supported))
	assert.Equal(t, "効きが弱いかも", settingsview.WeightTitle(promptweight.Weak))
	assert.Equal(t, "効きません（代わりに文で強調してください）", settingsview.WeightTitle(promptweight.Unsupported))
	assert.Equal(t, "未確認", settingsview.WeightTitle(promptweight.Unknown))
	assert.Equal(t, "未確認", settingsview.WeightTitle(""), "a workflow that does not say is not known")
}

// The models are grouped by the way they read weights, what works first; a way nobody has is left out, a name is said once, and the identifier never is.
func TestWeightGroupsSortTheModelsByDisplayName(t *testing.T) {
	groups := settingsview.WeightGroups([]modelcatalog.Info{
		model("Qwen-Image", promptweight.Unsupported),
		model("SDXL base", promptweight.Supported),
		model("Mine", ""),
		model("Qwen-Image 2.1", promptweight.Unsupported),
		model("SDXL base", promptweight.Supported),
		model("Juggernaut XL", promptweight.Supported),
	})

	var titles []string
	for _, g := range groups {
		titles = append(titles, g.Title)
	}
	assert.Equal(t, []string{"効きます", "効きません（代わりに文で強調してください）", "未確認"}, titles, "no weak group: nobody is weak")
	assert.Equal(t, []string{"SDXL base", "Juggernaut XL"}, groups[0].Names)
	assert.Equal(t, []string{"Qwen-Image", "Qwen-Image 2.1"}, groups[1].Names)
	assert.Equal(t, []string{"Mine"}, groups[2].Names)
	for _, g := range groups {
		assert.NotContains(t, strings.Join(g.Names, " "), "comfyui:", "an identifier is not a name")
	}
}

func TestWeightGroupsOfNoModelIsEmpty(t *testing.T) {
	assert.Empty(t, settingsview.WeightGroups(nil))
}
