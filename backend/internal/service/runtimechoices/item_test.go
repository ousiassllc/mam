package runtimechoices_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/choice"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime/lrtest"
	"github.com/ousiassllc/mam/backend/internal/service/runtimechoices"
	"github.com/ousiassllc/mam/backend/internal/service/runtimechoices/choicetest"
)

func speech(id string, langs []string, c modellicense.Commercial, vramGB int) runtimechoices.View {
	return runtimechoices.View{
		Choice:      choice.Choice{ID: id, Name: id, Kind: "tts", Languages: langs, VRAMMinGB: vramGB},
		LicenseInfo: catalog.ResourceLicense{Name: "MIT License", Commercial: c},
	}
}

func idsOf(views []runtimechoices.View) []string {
	out := []string{}
	for _, v := range views {
		out = append(out, v.ID)
	}
	return out
}

// 「音声」「日本語」で絞れること（Issue #1015・#1011）。
func TestItem_ASpeechModelIsAudioInItsLanguagesAndALanguageModelIsLLM(t *testing.T) {
	ja := speech("ja", []string{"ja"}, modellicense.Yes, 6)
	llm := runtimechoices.View{Choice: choice.Choice{ID: "llm", Name: "LLM", Kind: "llm", Languages: []string{"en"}, Uncensored: true}}

	assert.Equal(t, []modelfilter.Kind{modelfilter.KindAudio}, ja.Item().Kinds)
	assert.Equal(t, []string{"ja"}, ja.Item().Languages)
	assert.Equal(t, []modelfilter.Kind{modelfilter.KindLLM}, llm.Item().Kinds)
	assert.True(t, llm.Item().Uncensored)
	assert.True(t, llm.Item().Adult, "an uncensored model is an adult one")
	assert.True(t, ja.Item().Available, "every model of a runtime can be chosen")
	assert.Equal(t, modelfilter.OriginLocal, ja.Item().Origin)
}

func TestWhere_KeepsTheModelsOfTheKindAndLanguageInTheirOrder(t *testing.T) {
	small := speech("small", []string{"ja"}, modellicense.Yes, 6)
	large := speech("large", []string{"ja"}, modellicense.Conditional, 24)
	en := speech("en", []string{"en"}, modellicense.Yes, 4)
	llm := runtimechoices.View{Choice: choice.Choice{ID: "llm", Name: "LLM", Kind: "llm", Languages: []string{"ja", "en"}, VRAMMinGB: 8}, LicenseInfo: catalog.ResourceLicense{Commercial: modellicense.Yes}}
	all := []runtimechoices.View{small, large, en, llm}

	assert.Equal(t, []string{"small", "large", "en"}, idsOf(runtimechoices.Where(all, modelfilter.Condition{Kinds: []modelfilter.Kind{modelfilter.KindAudio}})))
	assert.Equal(t, []string{"small", "large", "llm"}, idsOf(runtimechoices.Where(all, modelfilter.Condition{Languages: []string{"ja"}})))
	assert.Equal(t, []string{"small", "large"}, idsOf(runtimechoices.Where(all, modelfilter.Condition{Kinds: []modelfilter.Kind{modelfilter.KindAudio}, Languages: []string{"ja"}})))
	assert.Equal(t, []string{"small"}, idsOf(runtimechoices.Where(all, modelfilter.Condition{Kinds: []modelfilter.Kind{modelfilter.KindAudio}, Languages: []string{"ja"}, VRAMFit: true, Commercial: "ok"})), "ja speech, 16 GB, commercial ok")
	assert.Equal(t, []string{"large"}, idsOf(runtimechoices.Where(all, modelfilter.Condition{Commercial: "conditional"})))
	assert.Equal(t, []string{"small", "large", "en", "llm"}, idsOf(runtimechoices.Where(all, modelfilter.Condition{})), "no condition keeps every model")
}

func TestWhere_TheModelsOfARuntimeInTheCatalogAreAudioAndJapanese(t *testing.T) {
	s, _ := newService(t)
	views, err := s.Views(context.Background(), lrtest.ID)
	require.NoError(t, err)

	got := runtimechoices.Where(views, modelfilter.Condition{Kinds: []modelfilter.Kind{modelfilter.KindAudio}, Languages: []string{"ja"}})

	assert.ElementsMatch(t, []string{choicetest.SmallChoice, choicetest.LargeChoice}, idsOf(got))
	assert.Empty(t, runtimechoices.Where(views, modelfilter.Condition{Languages: []string{"en"}}))
}
