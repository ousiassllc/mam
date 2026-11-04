package apperr

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A text a language does not have is read in Japanese, the base: a new text can be written in Japanese first and translated later.
func TestTextFallsBackToJapaneseForAnUntranslatedKey(t *testing.T) {
	messagesJa["test.only_ja"] = "日本語だけ {name}"
	t.Cleanup(func() { delete(messagesJa, "test.only_ja") })

	assert.Equal(t, "日本語だけ x", Text(LangEn, "test.only_ja", map[string]string{"name": "x"}))
	assert.Equal(t, "日本語だけ x", Text(LangJa, "test.only_ja", map[string]string{"name": "x"}))
	assert.Equal(t, "日本語だけ", Text(Lang("fr"), "test.only_ja", nil), "a language the catalog does not know reads Japanese")
	assert.Equal(t, "test.nowhere", Text(LangEn, "test.nowhere", nil), "a key no language has shows as itself")
}

// The English catalog holds no key the Japanese base lacks (a text written for English only would show as its key in Japanese), and every
// {{key}} a text refers to exists in the base.
func TestCatalogsAreConsistent(t *testing.T) {
	for k := range messagesEn {
		_, ok := messagesJa[k]
		assert.True(t, ok, "%s is in the English catalog but not in the Japanese base", k)
	}
	for lang, catalog := range catalogs {
		for k, text := range catalog {
			for _, m := range token.FindAllString(text, -1) {
				_, ok := messagesJa[m[2:len(m)-2]]
				assert.True(t, ok, "%s %s refers to %s, which the base lacks", lang, k, m)
			}
		}
	}
}
