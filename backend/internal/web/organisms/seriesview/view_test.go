package seriesview_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/seriesview"
)

func renderHTML(t *testing.T, c templ.Component) string {
	t.Helper()
	var sb strings.Builder
	require.NoError(t, c.Render(context.Background(), &sb))
	return sb.String()
}

func seriesViewData() seriesview.Data {
	return seriesview.Data{
		Series: domain.Series{ID: "s1", Name: "Mio", Style: "watercolor", Version: 3},
		Canon: domain.SeriesCanon{Characters: []domain.WorldCharacter{
			{ID: "c1", Name: "Mio", Wardrobe: "red coat"},
			{ID: "c2", Name: "Ren"},
		}},
		Versions: []domain.SeriesVersion{{Number: 3, CreatedAt: time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)}},
		Episodes: []domain.Project{{ID: "p1", Name: "pilot", EpisodeNumber: 1}},
	}
}

// Saving one Character answers with the form of that Character, and the version line and the history out of band.
// The other Characters, the Style and the Episode form share the screen: replacing them would discard what the user
// typed into them without a word (#693, the same shape as #685).
func TestSeriesCharacterSaved_AnswersTheFormOfThatCharacterOnly(t *testing.T) {
	html := renderHTML(t, seriesview.CharacterSaved(seriesViewData(), "c1"))

	assert.Contains(t, html, `id="series-character-c1"`)
	assert.Contains(t, html, `hx-target="this"`)
	assert.Contains(t, html, `red coat`)
	assert.Contains(t, html, `<p id="series-version" class="text-xs text-neutral-400" hx-swap-oob="true">`)
	assert.Contains(t, html, `<section id="series-history" aria-label="History" class="flex flex-col gap-1" hx-swap-oob="true">`)
	assert.Contains(t, html, "バージョン 3")
	for _, other := range []string{`id="series-view"`, `id="series-character-c2"`, `id="series-style-form"`, `name="style"`, `name="previous_episode_id"`, `series/s1/episodes`} {
		assert.NotContains(t, html, other)
	}
}

func TestSeriesStyleSaved_AnswersTheStyleFormOnly(t *testing.T) {
	html := renderHTML(t, seriesview.StyleSaved(seriesViewData()))

	assert.Contains(t, html, `id="series-style-form"`)
	assert.Contains(t, html, `hx-target="this"`)
	assert.Contains(t, html, `value="watercolor"`)
	assert.Contains(t, html, `<p id="series-version" class="text-xs text-neutral-400" hx-swap-oob="true">`)
	assert.Contains(t, html, `<section id="series-history" aria-label="History" class="flex flex-col gap-1" hx-swap-oob="true">`)
	for _, other := range []string{`id="series-view"`, `id="series-character-c1"`, `id="series-character-c2"`, `name="previous_episode_id"`, `series/s1/episodes`} {
		assert.NotContains(t, html, other)
	}
}

// The page carries every form with its own id and replaces only itself, so a save never reaches the others.
func TestSeriesView_EachFormReplacesItself(t *testing.T) {
	html := renderHTML(t, seriesview.View(seriesViewData()))

	for _, id := range []string{"series-style-form", "series-character-c1", "series-character-c2", "series-version", "series-history"} {
		assert.Contains(t, html, `id="`+id+`"`)
	}
	assert.NotContains(t, html, `hx-target="#series-view"`)
	assert.NotContains(t, html, `hx-swap-oob`)
}
