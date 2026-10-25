package settingsui_test

import (
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/settingsui"
)

func render(t *testing.T, c templ.Component) string {
	t.Helper()
	var sb strings.Builder
	require.NoError(t, c.Render(context.Background(), &sb))
	return sb.String()
}

// The current category is told by aria-current and, not by colour alone, by a heavier weight and a border; the others are
// plain links (no hx-boost: a move is a page load).
func TestNav_MarksOnlyTheCurrentCategoryAndLinksAreNotBoosted(t *testing.T) {
	html := render(t, settingsui.Nav([]settingsui.NavItem{
		{Href: "/settings", Label: "概要"},
		{Href: "/settings/api-keys", Label: "API キーとログイン", Current: true},
		{Href: "/settings/update", Label: "バージョンとアップデート"},
	}))

	assert.Contains(t, html, `<nav aria-label="設定"`)
	assert.Equal(t, 1, strings.Count(html, `aria-current="page"`))
	assert.Equal(t, 3, strings.Count(html, "<a href="))
	assert.NotContains(t, html, "hx-")
	current := html[strings.Index(html, `href="/settings/api-keys"`):]
	current = current[:strings.Index(current, "</a>")]
	assert.Contains(t, current, `aria-current="page"`)
	assert.Contains(t, current, "font-medium")
	assert.Contains(t, current, "border-indigo-400")
	assert.NotContains(t, html, "text-neutral-500")
}

func TestCategory_IsTheH2OfItsContentAndNamedByIt(t *testing.T) {
	ctx := templ.WithChildren(context.Background(), templ.Raw("<p>CONTENT</p>"))
	var sb strings.Builder
	require.NoError(t, settingsui.Category("概要", "説明").Render(ctx, &sb))
	html := sb.String()

	assert.Contains(t, html, `<section id="settings-category" aria-labelledby="settings-category-title"`)
	assert.Contains(t, html, `<h2 id="settings-category-title"`)
	assert.Less(t, strings.Index(html, "概要"), strings.Index(html, "CONTENT"))
	assert.Contains(t, html, "説明")
}

// A card is one list item with the id that a save replaces; the title is a heading of the level given, the state is a
// badge with words, and out of band replacement is asked for only on request.
func TestCardItem_IsAListItemWithItsIDStateAndButtonsAndReplacesItselfOnlyWhenAsked(t *testing.T) {
	card := settingsui.Card{
		ID: "credential-row-kling", TestID: "credential-row", Attrs: templ.Attributes{"data-provider": "kling"},
		Title: "Kling", Tone: atoms.BadgeOK, State: "設定済み", Description: "動画生成", Summary: "要約", Notice: "保存しました",
	}
	ctx := templ.WithChildren(context.Background(), templ.Raw("<button>操作</button>"))
	renderCard := func(c settingsui.Card) string {
		var sb strings.Builder
		require.NoError(t, settingsui.CardItem(c).Render(ctx, &sb))
		return sb.String()
	}

	html := renderCard(card)
	assert.Contains(t, html, `<li id="credential-row-kling" data-testid="credential-row"`)
	assert.Contains(t, html, `data-provider="kling"`)
	assert.Contains(t, html, `<h3 class="text-sm font-semibold text-neutral-100">Kling</h3>`)
	assert.Contains(t, html, "設定済み")
	assert.Contains(t, html, `role="status">保存しました`)
	assert.Contains(t, html, "<button>操作</button>")
	assert.NotContains(t, html, "hx-swap-oob")
	assert.NotContains(t, html, "text-neutral-500")

	card.OOB, card.TitleLevel = true, 4
	html = renderCard(card)
	assert.Contains(t, html, `hx-swap-oob="true"`)
	assert.Contains(t, html, "<h4 ")
	assert.NotContains(t, html, "<h3 ")
}

func TestCardItem_DefaultTestIDAndNoEmptyParts(t *testing.T) {
	var sb strings.Builder
	require.NoError(t, settingsui.CardItem(settingsui.Card{ID: "x-1", Title: "X"}).Render(context.Background(), &sb))

	assert.Contains(t, sb.String(), `data-testid="settings-card"`)
	assert.NotContains(t, sb.String(), "<p ", "no description, summary or notice: no empty paragraph")
}
