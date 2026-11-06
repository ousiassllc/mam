package reftest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// #928: every model card says whether the model may be used commercially, by the badge with its word and by the icon
// the filter bar is drawn above the cards and follows the setting.
func TestComposeShowsTheCommercialBadgeOnEveryModelCardAndTheFilter(t *testing.T) {
	s := newScreen(t)

	for media, ids := range map[string][]string{"image": {"flux-2", "gpt-image-2", "nano-banana"}, "video": {"luma-ray-3.2", "kling-3.0", "runway-gen-4.5"}} {
		page := s.page(paths.GenerationTest() + "?tier=detail&media=" + media)
		for _, id := range ids {
			c := card(t, page, id)
			info := modellicense.Of(id)
			assert.Contains(t, c, `data-commercial-badge="`+string(info.Commercial)+`"`, id)
			assert.Contains(t, c, info.Commercial.Label(), id)
		}
	}
	page := s.page(paths.GenerationTest() + "?tier=detail&media=video")
	assert.Contains(t, page, `data-testid="model-pick-bar"`, "the same filter bar as every list of models")
	assert.NotContains(t, page, `name="commercial" value="all"`, "すべて is the setting's while it is off: the screen follows the setting")
	assert.Contains(t, card(t, page, "luma-ray-3.2"), "サブスクリプション・契約", "a conditional card says its condition in words")
}

// With the setting 商用OKのモデルだけを使う on the filter starts at 「商用OKのみ」: the server narrows the list to the models that are commercial OK.
func TestComposeDrawsTheFilterAtTheSettingsDefault(t *testing.T) {
	modellicense.SetDefaultFilter(func() modellicense.Filter { return modellicense.FilterOK })
	t.Cleanup(func() { modellicense.SetDefaultFilter(nil) })
	s := newScreen(t)

	page := s.page(paths.GenerationTest() + "?tier=detail&media=video")

	assert.NotContains(t, page, `name="commercial" value="ok"`, "商用OKのみ is the setting's: sent as the empty word")
	assert.Contains(t, page, `name="commercial" value="all"`, "すべて is a choice of its own now")
	assert.NotContains(t, page, `data-model="luma-ray-3.2"`, "a model whose answer is not 商用OK is not listed")
	assert.Contains(t, page, `data-model="kling-3.0"`, "a 商用OK model is")
}

func TestTheModelCardsOfTheLocalModelsSayUnknownUntilTheModelManagerKnowsTheirLicense(t *testing.T) {
	s := newScreen(t, gentesttest.WithNoReferenceModel())

	page := s.page(paths.GenerationTest() + "?tier=detail&media=image")

	c := card(t, page, string(gentesttest.NoReferenceModel))
	assert.Contains(t, c, "不明")
}
