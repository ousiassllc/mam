package pages_test

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
	"github.com/ousiassllc/mam/backend/internal/web/pages"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

func renderProjectsPage(t *testing.T, projects ...domain.Project) string {
	t.Helper()
	var sb strings.Builder
	require.NoError(t, pages.ProjectsPage("tok", projects, organisms.NewProjectFormData{}).Render(context.Background(), &sb))
	return sb.String()
}

// The tools are seven cards in three groups under the Project list; the status of five of them is read after the page is
// shown (spec/components/home-screen.md §3, §5).
func TestProjectsPage_HasSevenToolCardsInThreeGroupsWithAStatusLoadedLater(t *testing.T) {
	html := renderProjectsPage(t, domain.Project{ID: "p1", Name: "作品"})

	cards := regexp.MustCompile(`<a id="tool-([a-z-]+)" href="([^"]+)"`).FindAllStringSubmatch(html, -1)
	got := map[string]string{}
	var order []string
	for _, c := range cards {
		got[c[1]] = c[2]
		order = append(order, c[1])
	}
	assert.Equal(t, []string{"generation-test", "nodes", "workflows", "model-manager", "settings", "analytics", "reviews"}, order)
	assert.Equal(t, map[string]string{
		"generation-test": paths.GenerationTest(), "nodes": paths.Nodes(), "workflows": paths.Workflows(), "model-manager": paths.ModelManager(),
		"settings": paths.Settings(), "analytics": paths.Analytics(), "reviews": paths.Reviews(),
	}, got)

	assert.Equal(t, 3, strings.Count(html, `<ul role="list" aria-labelledby="tool-group-`), "three groups, each a list named by its heading")
	for _, tool := range []string{paths.ToolNodes, paths.ToolWorkflows, paths.ToolModelManager, paths.ToolSettings, paths.ToolReviews} {
		assert.Contains(t, html, `hx-get="`+paths.ToolStatus(tool)+`" hx-trigger="load"`, tool)
	}
	assert.Equal(t, 5, strings.Count(html, `hx-trigger="load"`), "the generation test and the analytics have no status")
}

// The text links the cards replace are gone, the reviews link included, and the Project list stays.
func TestProjectsPage_TheTextLinksAreGone(t *testing.T) {
	html := renderProjectsPage(t, domain.Project{ID: "p1", Name: "作品"})

	for _, old := range []string{
		"受け取ったレビューパッケージを開く（レビュアー向け）", "制作指標（すべてのプロジェクト）", "設定（API キー・モデル）",
		"モデルマネージャー（ローカルモデルの導入・試し生成）", "生成テスト（モデルとプロンプトを並べて比べる）", "ノード（ComfyUI のワークフロー）",
	} {
		assert.NotContains(t, html, old)
	}
	for _, p := range []string{paths.Reviews(), paths.Analytics(), paths.Settings(), paths.ModelManager(), paths.GenerationTest(), paths.Nodes(), paths.Workflows()} {
		assert.Equal(t, 1, strings.Count(html, `href="`+p+`"`), "%s is linked once, by its card", p)
	}
	assert.Contains(t, html, "作品")
}

func TestProjectsPage_WithoutProjectsTheCardsAreTheSame(t *testing.T) {
	with := renderProjectsPage(t, domain.Project{ID: "p1", Name: "作品"})
	without := renderProjectsPage(t)

	assert.Equal(t, strings.Count(with, `data-testid="tool-card"`), strings.Count(without, `data-testid="tool-card"`))
	assert.Equal(t, 7, strings.Count(without, `data-testid="tool-card"`))
}

// The whole page is one column of max-w-5xl (no narrower wrapper around the forms and the list), and the tool cards are
// three columns at every width (spec/components/home-screen.md §2, §4, §9 question 2).
func TestProjectsPage_IsOneWideColumnWithThreeCardColumns(t *testing.T) {
	html := renderProjectsPage(t, domain.Project{ID: "p1", Name: "作品"})

	assert.Contains(t, html, `<main class="mx-auto max-w-5xl p-8">`)
	assert.NotContains(t, html, "max-w-3xl", "the heading, the forms and the list are not in a narrower wrapper")
	assert.Equal(t, 3, strings.Count(html, `class="mt-2 grid grid-cols-3 gap-4"`), "each of the three groups is three columns")
	assert.NotContains(t, html, "grid-cols-2")
	assert.NotContains(t, html, "xl:grid-cols")
}
