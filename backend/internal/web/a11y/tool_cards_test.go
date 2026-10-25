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
	return sb.String()
}

// Each card of the home page is a link named by its title alone, so that a link list or a voice command ("click
// 設定") tells them apart and the visible name is the accessible name (WCAG 2.5.3). The description and the status
// are its description, and what they refer to exists.
func TestHomeToolCards_AreNamedByTheirTitleAndTheDescriptionsExist(t *testing.T) {
	html := homePage(t)
	var names []string
	for _, m := range regexp.MustCompile(`<a id="tool-[a-z-]+"[^>]*aria-labelledby="([^"]+)" aria-describedby="([^"]+)"`).FindAllStringSubmatch(html, -1) {
		title := regexp.MustCompile(`id="` + m[1] + `"[^>]*>([^<]+)</span>`).FindStringSubmatch(html)
		require.NotNil(t, title, "the title %s that names the link exists", m[1])
		names = append(names, title[1])
		for _, id := range strings.Fields(m[2]) {
			assert.Contains(t, html, `id="`+id+`"`, "the description %s exists", id)
		}
	}

	assert.Equal(t, []string{"生成テスト", "ノード", "ワークフロー", "モデルマネージャー", "設定", "制作指標", "レビューパッケージ"}, names)
}

// h1 (mam) → h2 (Projects, Tools) → h3 (the groups): no level is skipped (#781).
func TestHomePage_HeadingsDoNotSkipALevel(t *testing.T) {
	var levels []string
	for _, m := range regexp.MustCompile(`<h([1-6])[ >]`).FindAllStringSubmatch(homePage(t), -1) {
		levels = append(levels, m[1])
	}

	assert.Equal(t, []string{"1", "2", "2", "3", "3", "3"}, levels)
}

// A card is focusable by its link alone and shows where the focus is: the ring of indigo-400 on the neutral-950 page
// (6.64:1) with a gap, as the other focus rings of the app; the text colours are the ones contrast_test accepts.
func TestHomeToolCards_ShowTheFocusAndKeepTheirTextReadable(t *testing.T) {
	html := homePage(t)

	card := regexp.MustCompile(`<a id="tool-nodes"[^>]*class="([^"]+)"`).FindStringSubmatch(html)
	require.NotNil(t, card)
	for _, c := range []string{"focus-visible:ring-2", "focus-visible:ring-indigo-400", "focus-visible:ring-offset-neutral-950", "motion-reduce:transition-none"} {
		assert.Contains(t, card[1], c)
	}
	assert.Equal(t, 7, strings.Count(html, `data-testid="tool-card"`))
	assert.NotContains(t, html, "tabindex")
	assert.NotContains(t, html, "text-neutral-500")
}
