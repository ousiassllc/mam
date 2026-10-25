package molecules_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/molecules"
)

func card() molecules.ToolCard {
	return molecules.ToolCard{
		ID: "nodes", Href: "/nodes", Icon: atoms.IconNetwork, Title: "ノード", Description: "ComfyUI のノード画面でワークフローを作る",
		StatusURL: "/tools/nodes/status",
	}
}

// The card is one link named by its title alone; the description and the status are its description, and nothing
// focusable or clickable sits inside it (WCAG 2.5.3, spec/components/shared-ui-parts.md §3).
func TestToolCard_IsOneLinkNamedByItsTitle(t *testing.T) {
	html := render(t, molecules.ToolCardItem(card()))

	assert.Equal(t, 1, strings.Count(html, "<a "), "one link")
	assert.Contains(t, html, `href="/nodes"`)
	assert.Contains(t, html, `aria-labelledby="tool-nodes-title"`)
	assert.Contains(t, html, `aria-describedby="tool-nodes-desc tool-nodes-status"`)
	assert.Contains(t, html, `id="tool-nodes-title" class="text-base font-semibold text-neutral-100">ノード</span>`)
	for _, banned := range []string{"<button", "<input", "<select", "tabindex"} {
		assert.NotContains(t, html, banned)
	}
	for _, ref := range []string{"tool-nodes-title", "tool-nodes-desc", "tool-nodes-status"} {
		assert.Contains(t, html, `id="`+ref+`"`, "the id %s that the link refers to exists", ref)
	}
}

func TestToolCard_CTAIsOnlyDrawnAndDefaultsToOpen(t *testing.T) {
	html := render(t, molecules.ToolCardItem(card()))

	m := regexp.MustCompile(`<span class="inline-flex[^"]*" aria-hidden="true">\s*([^<]*)<svg`).FindStringSubmatch(html)
	require.NotNil(t, m, "the CTA is aria-hidden")
	assert.Equal(t, "開く", strings.TrimSpace(m[1]))

	c := card()
	c.CTA = "見る"
	assert.Contains(t, render(t, molecules.ToolCardItem(c)), "見る")
}

// The status is read after the page is shown, by the status box and not by the link, so that the link stays a plain link
// (middle click, "copy link"); the box is empty until the answer comes and is not a live region.
func TestToolCard_StatusIsLoadedByTheBoxAndNotTheLink(t *testing.T) {
	html := render(t, molecules.ToolCardItem(card()))

	link := regexp.MustCompile(`<a [^>]*>`).FindString(html)
	assert.NotContains(t, link, "hx-")
	box := regexp.MustCompile(`<span id="tool-nodes-status"[^>]*></span>`).FindString(html)
	require.NotEmpty(t, box, "the status box is empty")
	for _, attr := range []string{`hx-get="/tools/nodes/status"`, `hx-trigger="load"`, `hx-target="this"`, `hx-swap="innerHTML"`, "min-h-6"} {
		assert.Contains(t, box, attr)
	}
	assert.NotContains(t, box, "aria-live")
}

func TestToolCard_WithoutAStatusURLHasNoStatusBox(t *testing.T) {
	c := card()
	c.StatusURL = ""

	html := render(t, molecules.ToolCardItem(c))

	assert.NotContains(t, html, "tool-nodes-status")
	assert.Contains(t, html, `aria-describedby="tool-nodes-desc"`)
}

func TestToolStatus_DrawsOneBadgeForEachLineAndNothingForNone(t *testing.T) {
	html := render(t, molecules.ToolStatus([]molecules.StatusLine{
		{Tone: atoms.BadgeOK, Text: "ComfyUI: 起動中"},
		{Tone: atoms.BadgeWarn, Text: "ChatGPT: 再ログインが必要"},
	}))

	assert.Equal(t, 2, strings.Count(html, "<span"))
	assert.Contains(t, html, ">ComfyUI: 起動中</span>")
	assert.Contains(t, html, ">ChatGPT: 再ログインが必要</span>")
	assert.Empty(t, render(t, molecules.ToolStatus(nil)))
}
