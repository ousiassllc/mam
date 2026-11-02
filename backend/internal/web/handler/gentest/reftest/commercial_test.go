package reftest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// #928: every model card says whether the model may be used commercially, by the badge with its word and by data-commercial
// (which the filter reads); the filter is drawn above the cards, with 「すべて」 checked while the setting is off.
func TestComposeShowsTheCommercialBadgeOnEveryModelCardAndTheFilter(t *testing.T) {
	s := newScreen(t)

	for media, ids := range map[string][]string{"image": {"flux-2", "gpt-image-2", "nano-banana"}, "video": {"luma-ray-3.2", "kling-3.0", "runway-gen-4.5"}} {
		page := s.page(paths.GenerationTest() + "?tier=detail&media=" + media)
		for _, id := range ids {
			c := card(t, page, id)
			info := modellicense.Of(id)
			assert.Contains(t, c, `data-commercial="`+string(info.Commercial)+`"`, id)
			assert.Contains(t, c, `data-commercial-badge="`+string(info.Commercial)+`"`, id)
			assert.Contains(t, c, info.Commercial.Label(), id)
		}
	}
	page := s.page(paths.GenerationTest() + "?media=video")
	assert.Contains(t, page, `data-commercial-filter-target="#gt-models"`)
	assert.Regexp(t, `value="all"\s+checked`, page, "すべて is checked while the setting is off")
	assert.Contains(t, card(t, page, "luma-ray-3.2"), "サブスクリプション・契約", "a conditional card says its condition in words")
	assert.Contains(t, card(t, page, "kling-3.0"), "ライセンスを確認できていません", "an unknown answer is said in words, not only by the badge")
}

// With the setting 商用OKのモデルだけを使う on the filter is drawn with 「商用OKのみ」 checked: the script then hides the other cards.
func TestComposeDrawsTheFilterAtTheSettingsDefault(t *testing.T) {
	modellicense.SetDefaultFilter(func() modellicense.Filter { return modellicense.FilterOK })
	t.Cleanup(func() { modellicense.SetDefaultFilter(nil) })
	s := newScreen(t)

	page := s.page(paths.GenerationTest() + "?media=video")

	assert.Regexp(t, `value="ok"\s+checked`, page)
	assert.NotRegexp(t, `value="all"\s+checked`, page)
}

func TestTheModelCardsOfTheLocalModelsSayUnknownUntilTheModelManagerKnowsTheirLicense(t *testing.T) {
	s := newScreen(t, gentesttest.WithNoReferenceModel())

	page := s.page(paths.GenerationTest() + "?tier=detail&media=image")

	c := card(t, page, string(gentesttest.NoReferenceModel))
	assert.Contains(t, c, `data-commercial="unknown"`, "a model of an endpoint the user registered: mam did not check it")
	assert.Contains(t, c, "不明")
}
