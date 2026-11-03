package nodecard_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/a-h/templ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	view "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/nodecard"
)

func render(t *testing.T, c templ.Component) string {
	t.Helper()
	var b bytes.Buffer
	require.NoError(t, c.Render(context.Background(), &b))
	return b.String()
}

// A model that needs a node the engine does not have says so on its card and cannot be installed from it: the button is disabled and the badge
// says why, instead of an install that the service refuses.
func TestAModelThatNeedsAMissingNodeHasADisabledInstallAndTheReason(t *testing.T) {
	m := view.Model{ID: "m", Name: "M", State: "not_installed", NeedsNodes: "追加ノード「IPAdapter plus」が導入されていません"}
	html := render(t, view.CatalogList(view.Catalog{Models: []view.Model{m}}))
	assert.Contains(t, html, `data-testid="mm-nodes-badge"`)
	assert.Contains(t, html, "追加ノード「IPAdapter plus」が導入されていません")
	assert.NotContains(t, html, `data-testid="mm-install"`)

	m.NeedsNodes = ""
	html = render(t, view.CatalogList(view.Catalog{Models: []view.Model{m}}))
	assert.NotContains(t, html, `data-testid="mm-nodes-badge"`)
	assert.Contains(t, html, `data-testid="mm-install"`)
}

// A node whose files changed after the install is not loaded: the card says so (it is not "導入済み") and offers to install it again and to remove it.
func TestAModifiedNodeIsNotShownAsInstalled(t *testing.T) {
	n := nodecard.Node{ID: "n", Name: "N", State: "modified", Detail: "ファイルが変わっています"}
	html := render(t, nodecard.NodeCard(n))
	assert.Contains(t, html, "内容が変わっています（読み込まれません）")
	assert.NotContains(t, html, "導入済み")
	assert.Contains(t, html, "導入し直す")
	assert.Contains(t, html, `data-testid="mm-node-remove"`)
}
