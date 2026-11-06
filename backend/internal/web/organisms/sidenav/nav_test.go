package sidenav_test

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/navstate"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/mmcat"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/sidenav"
	"github.com/ousiassllc/mam/backend/internal/web/themestate"
)

func render(ctx context.Context, t *testing.T, s sidenav.State) string {
	t.Helper()
	var sb strings.Builder
	require.NoError(t, sidenav.Nav(s).Render(ctx, &sb))
	return sb.String()
}

func TestNav_IsOneLandmarkWithALinkPerScreenAndTheCurrentOneMarked(t *testing.T) {
	html := render(t.Context(), t, sidenav.At(sidenav.GenerationTest))

	assert.Equal(t, 1, strings.Count(html, `<nav id="sidenav" aria-label="メニュー"`))
	assert.Equal(t, len(sidenav.Screens()), strings.Count(html, `data-testid="sidenav-item"`))
	assert.Equal(t, 1, strings.Count(html, `aria-current="page"`))
	assert.Contains(t, html, `data-key="generation-test" aria-current="page"`)
	assert.NotContains(t, html, `id="sidenav-views"`, "no Project: no views")
}

// A folded menu hides the label of an entry from the eye only: the link keeps its text for a screen reader, and carries the
// label as title for a mouse.
func TestNav_LabelsStayInTheTextOfTheLinksWhenFolded(t *testing.T) {
	for _, folded := range []bool{false, true} {
		html := render(navstate.With(t.Context(), folded), t, sidenav.At(sidenav.Projects))
		for _, it := range sidenav.Screens() {
			assert.Contains(t, html, `title="`+it.Label+`"`)
			assert.Contains(t, html, `truncate max-md:sr-only group-data-[folded=true]:sr-only">`+it.Label+`</span>`, "the text is there, visually hidden by the CSS")
		}
		assert.Regexp(t, `<svg[^>]*aria-hidden="true"`, html, "the icons are decoration")
	}
}

func TestNav_FoldStateIsDrawnFromTheRequestAndNamesTheButtonForIt(t *testing.T) {
	open := render(navstate.With(t.Context(), false), t, sidenav.At(sidenav.Projects))
	assert.Contains(t, open, `data-folded="false"`)
	assert.Contains(t, open, `aria-expanded="true" aria-label="`+sidenav.LabelFold+`"`)

	folded := render(navstate.With(t.Context(), true), t, sidenav.At(sidenav.Projects))
	assert.Contains(t, folded, `data-folded="true"`)
	assert.Contains(t, folded, `aria-expanded="false" aria-label="`+sidenav.LabelOpen+`"`)

	for _, html := range []string{open, folded} {
		assert.Contains(t, html, `data-cookie="`+navstate.CookieName+`"`, "the script writes the cookie the server reads")
		assert.Contains(t, html, `data-cookie-folded="`+navstate.Folded+`"`)
		assert.Contains(t, html, `aria-controls="sidenav"`)
	}
}

// The menu has the button that switches between the Light and the Dark theme; the script writes the cookie the server reads.
func TestNav_HasTheThemeButtonNamedForWhatItDoes(t *testing.T) {
	html := render(t.Context(), t, sidenav.At(sidenav.Projects))

	assert.Contains(t, html, `data-theme-toggle`)
	assert.Contains(t, html, `data-theme-cookie="`+themestate.CookieName+`"`)
	assert.Contains(t, html, `aria-label="`+sidenav.LabelTheme+`"`)
}

func TestNav_ProjectViewsFollowTheScreensAndSeriesOnlyForAnEpisode(t *testing.T) {
	p := domain.Project{ID: "p1", Name: "コーヒー店"}
	html := render(t.Context(), t, sidenav.InProject(sidenav.Brief, p))

	assert.Contains(t, html, `id="sidenav-views"`)
	assert.Contains(t, html, `aria-labelledby="sidenav-project"`, "the list of views is named by the Project")
	assert.Contains(t, html, `id="sidenav-project"`)
	assert.Contains(t, html, "コーヒー店")
	assert.Regexp(t, regexp.MustCompile(`data-key="brief" aria-current="page"`), html)
	assert.Equal(t, 1, strings.Count(html, `aria-current="page"`))
	assert.Less(t, strings.Index(html, `id="sidenav-screens"`), strings.Index(html, `id="sidenav-views"`))
	assert.Equal(t, len(sidenav.Screens())+len(sidenav.Views(p)), strings.Count(html, `data-testid="sidenav-item"`))
	assert.NotContains(t, html, `data-key="series"`)
}

func TestViews_SeriesIsOneMoreViewOfAnEpisode(t *testing.T) {
	p := domain.Project{ID: "p1", Name: "x", SeriesID: "s1"}
	items := sidenav.Views(p)

	assert.Equal(t, sidenav.Series, items[len(items)-1].Key)
	assert.Equal(t, "/series/s1", items[len(items)-1].Href)
}

func TestNav_HiddenDrawsNothing(t *testing.T) {
	assert.Empty(t, render(t.Context(), t, sidenav.Off()))
}

func TestNav_EveryEntryHasAUniqueKeyAndHref(t *testing.T) {
	seen := map[string]bool{}
	for _, it := range append(sidenav.Screens(), sidenav.Views(domain.Project{ID: "p", SeriesID: "s"})...) {
		assert.False(t, seen[string(it.Key)], "key %s", it.Key)
		assert.False(t, seen[it.Href], "href %s", it.Href)
		seen[string(it.Key)], seen[it.Href] = true, true
		assert.NotEmpty(t, it.Label)
		assert.NotEmpty(t, it.Icon)
	}
}

// The model manager's pages carry the second level under the screens: the heading names the list, the overview and the categories are entries, and an entry of the
// page that is shown is "page" (the screen's own entry says "true" on a category).
func TestNav_TheModelManagerHasASecondLevel(t *testing.T) {
	cats := mmcat.All()
	overview := render(t.Context(), t, sidenav.InModelManager("", cats))
	assert.Contains(t, overview, `id="sidenav-mm"`)
	assert.Contains(t, overview, `aria-labelledby="sidenav-mm"`)
	assert.Equal(t, len(sidenav.Screens())+1+len(cats), strings.Count(overview, `data-testid="sidenav-item"`))
	assert.Regexp(t, `data-key="model-manager" aria-current="page"`, overview)
	assert.Regexp(t, `data-key="model-manager:overview" aria-current="page"`, overview)
	assert.Equal(t, 2, strings.Count(overview, `aria-current="page"`))

	page := render(t.Context(), t, sidenav.InModelManager(mmcat.FaceCheck, cats))
	assert.Regexp(t, `data-key="model-manager" aria-current="true"`, page)
	assert.Regexp(t, `data-key="model-manager:face-check" aria-current="page"`, page)
	assert.Equal(t, 1, strings.Count(page, `aria-current="page"`))
	assert.NotContains(t, render(t.Context(), t, sidenav.At(sidenav.Projects)), `id="sidenav-mm"`)
}
