package a11y_test

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/organisms"
	"github.com/ousiassllc/mam/backend/internal/web/pages"
)

func homePage(t *testing.T) string {
	t.Helper()
	var sb strings.Builder
	require.NoError(t, pages.ProjectsPage("tok", nil, organisms.NewProjectFormData{}).Render(context.Background(), &sb))
	html := sb.String()
	return html[strings.Index(html, `id="content"`):] // the side menu has its own links (sidenav tests)
}

// h1 (mam) → h2 (Projects): no level is skipped (#781).
func TestHomePage_HeadingsDoNotSkipALevel(t *testing.T) {
	var levels []string
	for _, m := range regexp.MustCompile(`<h([1-6])[ >]`).FindAllStringSubmatch(homePage(t), -1) {
		levels = append(levels, m[1])
	}

	assert.Equal(t, []string{"1", "2"}, levels)
}
