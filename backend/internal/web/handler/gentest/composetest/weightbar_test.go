package composetest_test

import (
	"html"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// #977 / #972: the "強める + / 弱める −" of the selected words stands in the prompt fieldset of the easy and the detail tier, once each (a prompt
// row that is added later is inside it); the video form, which has no tiers, has none.
func TestThePromptsOfTheTiersHaveTheWeightBarAndTheVideoFormDoesNot(t *testing.T) {
	s := newScreen(t, gentesttest.WithTemplateModels())
	for _, tier := range []string{"easy", "detail"} {
		body := s.page(paths.GenerationTest() + "?tier=" + tier)
		fieldset := regexp.MustCompile(`(?s)<fieldset[^>]*data-testid="gt-prompt-fields".*?</fieldset>`).FindString(body)
		require.NotEmpty(t, fieldset, tier)
		assert.Equal(t, 1, len(regexp.MustCompile(`<mam-prompt-weight[ >]`).FindAllString(body, -1)), tier)
		assert.Contains(t, fieldset, "<mam-prompt-weight", tier+": it is in the prompt fieldset, for the script to find its prompt boxes")
	}
	easy := html.UnescapeString(s.page(paths.GenerationTest() + "?tier=easy"))
	assert.Contains(t, easy, "プロンプトのウェイトの説明", "the easy tier has the \"?\" of the weights too")
	assert.Contains(t, easy, "強める +", "…and its text says the buttons")

	video := s.get(paths.GenerationTestPart("form") + "?media=video").Body.String()
	assert.NotContains(t, video, "<mam-prompt-weight")
}

// The cards say, for the bar, how each model reads the weights in words: the same sentence as the "?" of the prompts.
func TestTheCardsCarryTheSentenceOfTheWeightsForTheBar(t *testing.T) {
	s := newScreen(t, gentesttest.WithTemplateModels())
	body := html.UnescapeString(s.page(paths.GenerationTest() + "?tier=detail"))
	card := regexp.MustCompile(`(?s)<li data-testid="gt-model" data-model="` + string(gentesttest.NoWeightModel) + `"[^>]*>`).FindString(body)
	require.NotEmpty(t, card)
	assert.Contains(t, card, `data-prompt-weight="unsupported"`)
	assert.Contains(t, card, `data-prompt-weight-text="効きません`)
	assert.Contains(t, card, "文で言い換え")
}
