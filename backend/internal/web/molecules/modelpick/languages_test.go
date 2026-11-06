package modelpick_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelpick"
)

// 「日本語」で絞る（Issue #1015・#1011）: the panel offers a section of languages when the screen's facets name some, and a language in force is a chip with an ×.
func TestLanguages_ThePanelOffersThemAndAChipSaysTheOneInForce(t *testing.T) {
	f := modelpick.Filter{
		ID: "bar", Action: "/models", Target: "#list", Facets: modelpick.Facets{Languages: []string{"en", "ja"}},
		Cond: modelfilter.Condition{Languages: []string{"ja"}},
	}

	var panel, summary strings.Builder
	require.NoError(t, modelpick.Panel(f, false).Render(context.Background(), &panel))
	require.NoError(t, modelpick.Summary(f).Render(context.Background(), &summary))

	assert.Contains(t, panel.String(), "日本語")
	assert.Contains(t, panel.String(), "英語")
	assert.Regexp(t, `name="languages" value="ja"[^>]* checked`, panel.String(), "the language in force is on")
	assert.NotRegexp(t, `name="languages" value="en"[^>]* checked`, panel.String())
	assert.Contains(t, summary.String(), "言語: 日本語")
	require.Len(t, f.Chips(), 1)
	assert.NotContains(t, f.Chips()[0].Remove, "languages=", "the address that removes the chip no longer asks for the language")
	assert.Equal(t, 1, f.Active())
}

func TestLanguages_APanelWithoutLanguagesHasNoSection(t *testing.T) {
	var panel strings.Builder
	require.NoError(t, modelpick.Panel(modelpick.Filter{ID: "bar", Facets: modelpick.Facets{Commercial: true}}, false).Render(context.Background(), &panel))

	assert.NotContains(t, panel.String(), `name="languages"`)
}

func TestLanguageLabel(t *testing.T) {
	assert.Equal(t, "日本語", modelpick.LanguageLabel("ja"))
	assert.Equal(t, "英語", modelpick.LanguageLabel("en"))
	assert.Equal(t, "fr", modelpick.LanguageLabel("fr"), "a code with no name is shown as it is")
}
