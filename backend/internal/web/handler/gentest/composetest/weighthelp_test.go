package composetest_test

import (
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// The names the model manager's catalog gives the three template models of the rig.
var catalogNames = map[string]string{
	string(gentesttest.SamplingModel):   "SDXL ベース",
	string(gentesttest.WeakWeightModel): "FLUX.1 dev",
	string(gentesttest.NoWeightModel):   "Qwen-Image",
}

func namedScreen(t *testing.T) *screen {
	return newScreen(t, gentesttest.WithTemplateModels(), gentesttest.WithCatalogInfo(func(id string) (modelfilter.CatalogInfo, bool) {
		name, ok := catalogNames[id]
		return modelfilter.CatalogInfo{Name: name}, ok
	}))
}

// panelOf is the explanation behind the "?" of the weights of the prompts (the popover of the detail tier's prompt fieldset).
func panelOf(t *testing.T, body string) string {
	panel := regexp.MustCompile(`(?s)<div id="help-[0-9a-f]+" popover[^>]*aria-label="ウェイトの説明".*?</details>`).FindString(body)
	require.NotEmpty(t, panel)
	return panel
}

// #1023: the "?" of the weights does not list every model with a sentence each. The models that are chosen now are named by the script (the
// <mam-weight-help> that reads the cards), and what the server draws is the way to write a weight and, in a fold, all the models by display name:
// one line for each way, each way's words said once, and no identifier.
func TestThePromptHelpHasTheIntroAndAFoldOfEveryModelByDisplayName(t *testing.T) {
	s := namedScreen(t)
	body := html.UnescapeString(s.page(paths.GenerationTest() + "?tier=detail"))
	panel := panelOf(t, body)

	assert.Contains(t, panel, "(語:1.3)", "how to write a weight")
	assert.Contains(t, panel, "<mam-weight-help", "the chosen models are said by the script")
	assert.NotContains(t, panel, "確かめていません", "no long sentence for a model that is not known")

	fold := regexp.MustCompile(`(?s)<details[^>]*data-testid="gt-weight-all".*?</details>`).FindString(panel)
	require.NotEmpty(t, fold)
	assert.Contains(t, fold, "すべてのモデルを見る")
	line := func(level string) string {
		return regexp.MustCompile(`(?s)<li[^>]*data-weight="` + level + `".*?</li>`).FindString(fold)
	}
	assert.Contains(t, line("supported"), "SDXL ベース")
	assert.Contains(t, line("supported"), "効きます")
	assert.Contains(t, line("weak"), "FLUX.1 dev")
	assert.Contains(t, line("weak"), "効きが弱いかも")
	assert.Contains(t, line("unsupported"), "Qwen-Image")
	assert.Contains(t, line("unsupported"), "効きません（代わりに文で強調してください）")
	assert.Contains(t, line("unknown"), "未確認", "a cloud model: one word")

	for _, title := range []string{"効きます", "効きが弱いかも", "効きません（代わりに文で強調してください）", "未確認"} {
		assert.Equal(t, 1, strings.Count(fold, title), "%q is said once for all the models that read the weights that way", title)
	}
	assert.NotContains(t, panel, "comfyui:", "an identifier is not a name")
	assert.NotContains(t, panel, "bundled-comfyui")
}

// The card is the name of the model and carries what the script says of it, in the same short words: no identifier on the card.
func TestTheCardsCarryTheDisplayNameAndTheShortWordsOfTheWeights(t *testing.T) {
	s := namedScreen(t)
	body := html.UnescapeString(s.page(paths.GenerationTest() + "?tier=detail"))
	card := func(id string) string {
		return regexp.MustCompile(`(?s)<li [^>]*data-model="` + id + `"[^>]*>`).FindString(body)
	}
	qwen := card(string(gentesttest.NoWeightModel))
	require.NotEmpty(t, qwen)
	assert.Contains(t, qwen, `data-label="Qwen-Image"`)
	assert.Contains(t, qwen, `data-prompt-weight="unsupported"`)
	assert.Contains(t, qwen, `data-prompt-weight-title="効きません（代わりに文で強調してください）"`)
	assert.Contains(t, card(string(gentesttest.SamplingModel)), `data-prompt-weight-title="効きます"`)
	assert.Contains(t, card("nano-banana"), `data-prompt-weight-title="未確認"`)
	assert.NotContains(t, body, `data-label="comfyui:`)
	assert.NotContains(t, body, "data-prompt-weight-text", "the sentence of each model is gone")
}

// A local model the catalog does not know (an external ComfyUI's) is still called by its own name, not by "comfyui:<endpoint>/<name>".
func TestAModelWithoutACatalogEntryIsCalledByItsName(t *testing.T) {
	s := newScreen(t, gentesttest.WithTemplateModels())
	body := html.UnescapeString(s.page(paths.GenerationTest() + "?tier=detail"))
	card := regexp.MustCompile(`(?s)<li [^>]*data-model="` + string(gentesttest.NoWeightModel) + `"[^>]*>`).FindString(body)
	require.NotEmpty(t, card)
	assert.Contains(t, card, `data-label="qwen-image"`)
}

// The easy tier's "?" of the prompt has the same help (its one model is the hidden card), and the caution under the prompt is on both tiers.
func TestBothTiersHaveTheWeightHelpAndTheCautionBesideThePrompt(t *testing.T) {
	s := namedScreen(t)
	for _, tier := range []string{"easy", "detail"} {
		body := html.UnescapeString(s.page(paths.GenerationTest() + "?tier=" + tier))
		assert.Equal(t, 1, strings.Count(body, "<mam-weight-help"), tier)
		assert.Equal(t, 1, strings.Count(body, "<mam-weight-note"), tier)
		fieldset := regexp.MustCompile(`(?s)<fieldset[^>]*data-testid="gt-prompt-fields".*?</fieldset>`).FindString(body)
		assert.Contains(t, fieldset, "<mam-weight-note", tier+": the caution is by the prompt boxes")
	}
}
