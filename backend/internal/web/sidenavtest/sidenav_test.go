package sidenavtest_test

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/sidenav"
	"github.com/ousiassllc/mam/backend/internal/web/screentest"
)

// currentEntries are the data-key of the entries of the menu that are marked aria-current="page".
func currentEntries(html string) []string {
	var keys []string
	for _, m := range regexp.MustCompile(`<a href="[^"]*" title="[^"]*" data-testid="sidenav-item" data-key="([^"]*)" aria-current="page"`).FindAllStringSubmatch(html, -1) {
		keys = append(keys, m[1])
	}
	return keys
}

// Every entry of the side menu is a page that answers 200, on the real router (the Wails app registers the same routes), and the
// page it leads to marks its own entry as the current one, and only that one (#915). The Project's views are listed
// only on the Project's own screens.
func TestSideMenu_EveryEntryLeadsToAPageThatMarksItAsCurrent(t *testing.T) {
	s := screentest.New(t)
	project, err := s.Svc.Projects.Create(t.Context(), service.CreateProjectInput{Content: "動画を作りたい"})
	require.NoError(t, err)

	for _, it := range sidenav.Screens() {
		if it.Key == sidenav.Workflows || it.Key == sidenav.Nodes {
			continue // only with the workflows service: handler/workflows/sidenav_test.go
		}
		t.Run("screen "+string(it.Key), func(t *testing.T) {
			rec := s.Visit(it.Href)
			require.Equal(t, http.StatusOK, rec.Code, it.Href)
			html := rec.Body.String()
			assert.Equal(t, []string{string(it.Key)}, currentEntries(html), "the entry of the page is the only current one")
			assert.NotContains(t, html, `id="sidenav-views"`, "a screen of the app lists no Project's views")
		})
	}

	for _, it := range sidenav.Views(project) {
		t.Run("view "+string(it.Key), func(t *testing.T) {
			rec := s.Visit(it.Href)
			require.Equal(t, http.StatusOK, rec.Code, it.Href)
			html := rec.Body.String()
			assert.Equal(t, []string{string(it.Key)}, currentEntries(html), "the entry of the view is the only current one")
			assert.Contains(t, html, `id="sidenav-views"`)
			for _, v := range sidenav.Views(project) {
				assert.Contains(t, html, `href="`+v.Href+`"`, "the other views of the Project are in the menu")
			}
		})
	}
}

// The menu is on every page of the app that is not the first-run guide: the same screens in the same order, one landmark.
func TestSideMenu_IsOnEveryScreenAsOneNavigationLandmark(t *testing.T) {
	s := screentest.New(t)
	for _, it := range sidenav.Screens() {
		if it.Key == sidenav.Workflows || it.Key == sidenav.Nodes {
			continue
		}
		html := s.Visit(it.Href).Body.String()
		assert.Equal(t, 1, strings.Count(html, `<nav id="sidenav" aria-label="メニュー"`), it.Href)
		last := -1
		for _, other := range sidenav.Screens() {
			at := strings.Index(html, `data-key="`+string(other.Key)+`"`)
			require.Greater(t, at, last, "%s: %s is after the one before it", it.Href, other.Key)
			last = at
		}
	}
}

// An unknown page is an error page with the menu (the way out is the menu), and no entry is current.
func TestSideMenu_ErrorPageHasNoCurrentEntry(t *testing.T) {
	s := screentest.New(t)
	rec := s.Visit("/projects/no-such-project")
	require.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), `data-testid="sidenav"`)
	assert.Empty(t, currentEntries(rec.Body.String()))
}
