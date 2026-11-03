package pages_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
	"github.com/ousiassllc/mam/backend/internal/web/pages"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// renderProjectsPage is the page without the side menu: the menu links to the screens of the app (side-menu.md), and
// these tests are about what the page itself holds.
func renderProjectsPage(t *testing.T, projects ...domain.Project) string {
	t.Helper()
	var sb strings.Builder
	require.NoError(t, pages.ProjectsPage("tok", projects, organisms.NewProjectFormData{}).Render(context.Background(), &sb))
	html := sb.String()
	return html[strings.Index(html, `id="content"`):]
}

// The page holds the new-Project form, the import form and the Project list only: the other screens are reached from the
// side menu, so no tool card and no status that the cards read after the page is shown (spec/components/home-screen.md §2).
func TestProjectsPage_HasNoToolCards(t *testing.T) {
	for _, html := range []string{renderProjectsPage(t, domain.Project{ID: "p1", Name: "作品"}), renderProjectsPage(t)} {
		assert.NotContains(t, html, `data-testid="tool-card"`)
		assert.NotContains(t, html, `hx-trigger="load"`)
		assert.NotContains(t, html, "ツール")
		for _, p := range []string{paths.Reviews(), paths.Analytics(), paths.Settings(), paths.ModelManager(), paths.GenerationTest(), paths.Nodes(), paths.Workflows()} {
			assert.NotContains(t, html, `href="`+p+`"`, "%s is reached from the side menu", p)
		}
	}
}

func TestProjectsPage_ListsTheProjects(t *testing.T) {
	assert.Contains(t, renderProjectsPage(t, domain.Project{ID: "p1", Name: "作品"}), "作品")
}

// The whole page is one column of max-w-5xl, with no narrower wrapper around the forms and the list
// (spec/components/home-screen.md §2).
func TestProjectsPage_IsOneWideColumn(t *testing.T) {
	html := renderProjectsPage(t, domain.Project{ID: "p1", Name: "作品"})

	assert.Contains(t, html, `<main class="mx-auto flex w-full flex-col gap-6 p-4 md:p-8 max-w-5xl">`)
	assert.NotContains(t, html, "max-w-3xl", "the heading, the forms and the list are not in a narrower wrapper")
}
