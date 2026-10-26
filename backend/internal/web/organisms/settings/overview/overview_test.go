package overview_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/molecules"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/settings/overview"
)

// The overview is one list of cards, each one link to its category, in the order given; a state the server knows is in the
// card and one it does not is read after the page is shown.
func TestOverview_IsAListOfCardsThatLinkToTheirCategories(t *testing.T) {
	var sb strings.Builder
	require.NoError(t, overview.Overview(overview.Data{Cards: []molecules.ToolCard{
		{ID: "api-keys", Href: "/settings/api-keys", Icon: atoms.IconKey, Title: "API キーとログイン", Description: "キー", Status: []molecules.StatusLine{{Tone: atoms.BadgeOK, Text: "API キー 2 / 10 件"}}},
		{ID: "local-models", Href: "/settings/local-models", Icon: atoms.IconLocal, Title: "ローカル", Description: "接続先", StatusURL: "/settings/local-models/health"},
	}}).Render(context.Background(), &sb))
	html := sb.String()

	assert.Contains(t, html, `<ul id="settings-overview" role="list"`)
	assert.Equal(t, 2, strings.Count(html, "<li"))
	assert.Less(t, strings.Index(html, `href="/settings/api-keys"`), strings.Index(html, `href="/settings/local-models"`))
	assert.Contains(t, html, "API キー 2 / 10 件")
	assert.Contains(t, html, `hx-get="/settings/local-models/health"`)
	assert.Contains(t, html, `hx-trigger="load"`)
}
