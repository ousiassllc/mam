package modelfilter_test

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelfilter"
)

// 「音声」「日本語」で絞る（Issue #1015・#1011）: a speaking model has a kind (audio) and the languages it speaks; the models that have no language never match one.
func TestLanguagesNarrowTheSpeakingModelsAndNoOtherModel(t *testing.T) {
	speak := func(id string, langs ...string) modelfilter.Item {
		return item(id, func(i *modelfilter.Item) { i.Kinds, i.Languages = []modelfilter.Kind{modelfilter.KindAudio}, langs })
	}
	ja, en, both := speak("ja", "ja"), speak("en", "en"), speak("both", "ja", "en")
	image := item("image", nil)
	all := []modelfilter.Item{ja, en, both, image}

	assert.Equal(t, []string{"ja", "both"}, apply(t, modelfilter.Condition{Languages: []string{"ja"}}, all...))
	assert.Equal(t, []string{"ja", "en", "both"}, apply(t, modelfilter.Condition{Languages: []string{"ja", "en"}}, all...), "languages are any-of")
	assert.Equal(t, []string{"ja", "en", "both"}, apply(t, modelfilter.Condition{Kinds: []modelfilter.Kind{modelfilter.KindAudio}}, all...))
	assert.Equal(t, []string{"ja", "both"}, apply(t, modelfilter.Condition{Kinds: []modelfilter.Kind{modelfilter.KindAudio}, Languages: []string{"ja"}}, all...), "kind and language combine")
	assert.Empty(t, apply(t, modelfilter.Condition{Languages: []string{"zh"}}, all...))
	assert.Equal(t, 2, modelfilter.Condition{Kinds: []modelfilter.Kind{modelfilter.KindAudio}, Languages: []string{"ja"}}.Active(), "the language is a part of its own on the funnel's count")
}

func TestLanguagesAreWordsOfTheQueryAndTheJSON(t *testing.T) {
	c, err := modelfilter.ParseQuery(url.Values{"languages": {"JA, en", "ja"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"ja", "en"}, c.Languages, "lower case, without repeats, in the order given")
	assert.Equal(t, "ja,en", c.Query(modelfilter.Condition{}).Get("languages"))
	assert.Contains(t, modelfilter.Keys(), "languages")

	data, err := c.JSON()
	require.NoError(t, err)
	fromJSON, err := modelfilter.ParseJSON(data)
	require.NoError(t, err)
	assert.Equal(t, c, fromJSON)

	for _, bad := range []string{"japanese", "j", "j1", "日本語"} {
		_, err := modelfilter.ParseQuery(url.Values{"languages": {bad}})
		if assert.Error(t, err, bad) {
			assert.Contains(t, err.Error(), "languages:", bad)
		}
	}
}
