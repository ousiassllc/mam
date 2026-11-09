package nodecard_test

import (
	"bytes"
	"context"
	"html"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/nodecat"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/nodecard"
)

func render(t *testing.T, c templ.Component) string {
	t.Helper()
	var b bytes.Buffer
	require.NoError(t, c.Render(context.Background(), &b))
	return b.String()
}

// A model that needs a node the engine does not have says so with a badge on its card; its install button opens the popup with the reason (#1118) and
// posts nothing. Another thing that stops an install (nothing can be installed now, a full disk) keeps a disabled button that opens nothing.
func TestAModelThatNeedsAMissingNodeHasAnInstallButtonThatOpensTheReason(t *testing.T) {
	m := view.Model{ID: "m", Name: "M", State: "not_installed", NeedsNodes: "追加ノード「IPAdapter plus」が導入されていません"}
	html := render(t, view.CatalogList(view.Catalog{Models: []view.Model{m}}))
	assert.Contains(t, html, `data-testid="mm-nodes-badge"`)
	assert.NotContains(t, html, "IPAdapter plus", "the long explanation is not on the card (#1118)")
	assert.NotContains(t, html, `data-testid="mm-install"`)
	assert.NotContains(t, html, `hx-post=`, "nothing posts an install")
	assert.Equal(t, 1, strings.Count(html, `data-testid="mm-reason-open"`), "one button opens the reason")
	assert.Regexp(t, `<button[^>]*data-testid="mm-reason-open"[^>]*>導入</button>`, html)
	assert.NotRegexp(t, `<button[^>]*\sdisabled[\s>=][^>]*>導入</button>`, html, "the install button can be pressed")
	_, row, _ := strings.Cut(html, "relative z-raised mt-auto")
	assert.Equal(t, 1, strings.Count(row, "<button"), "one button in the row: the install button, no second one for the reason")

	for name, c := range map[string]view.Catalog{
		"nothing can be installed now": {Models: []view.Model{m}, Blocked: "先に ComfyUI エンジンを導入してください"},
	} {
		html = render(t, view.CatalogList(c))
		assert.NotContains(t, html, `data-testid="mm-reason-open"`, name)
		assert.Regexp(t, `<button[^>]*\sdisabled[\s>=][^>]*>導入</button>`, html, name)
	}
	m.LowDisk = true
	html = render(t, view.CatalogList(view.Catalog{Models: []view.Model{m}}))
	assert.Contains(t, html, `data-testid="mm-reason-open"`, "a full disk does not take the reason away: the install button still explains")
	m.NeedsNodes = ""
	html = render(t, view.CatalogList(view.Catalog{Models: []view.Model{m}}))
	assert.NotContains(t, html, `data-testid="mm-reason-open"`)
	assert.Regexp(t, `<button[^>]*\sdisabled[\s>=][^>]*>導入</button>`, html, "a full disk alone keeps the button disabled")

	m.LowDisk = false
	html = render(t, view.CatalogList(view.Catalog{Models: []view.Model{m}}))
	assert.NotContains(t, html, `data-testid="mm-nodes-badge"`)
	assert.NotContains(t, html, `data-testid="mm-reason-open"`)
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

// The card of a node does not say what the node is: the description (156 to 407 characters for each node of the allowlist) is the description of the
// detail modal the card opens, and the card keeps what a decision about the install needs (#1103). Every node of the allowlist is drawn, as a whole section
// and as the card a poll swaps in.
func TestNodeCard_HasNoDescriptionButKeepsWhatDecidesTheInstall(t *testing.T) {
	all := nodecat.Default().Nodes
	require.Len(t, all, 5)
	var items []nodecard.Node
	for _, c := range all {
		n := nodecard.Node{ID: c.ID, Name: c.Name, Description: c.Description, Size: "1 MiB", State: "not_installed", Models: []nodecard.Model{{Name: "M"}}}
		if c.InstallBlocked != "" {
			n.State, n.Blocked = "blocked", c.InstallBlocked
		}
		items = append(items, n)
	}
	section := render(t, nodecard.NodesSection(nodecard.Section{Items: items}))
	assert.NotContains(t, section, "mm-node-description")
	for _, n := range items {
		card := render(t, nodecard.NodeCard(n))
		for name, body := range map[string]string{"the section": section, "the card": card} {
			assert.NotContains(t, body, n.Description, "%s of %s", name, n.ID)
			assert.NotContains(t, body, html.EscapeString(n.Description), "%s of %s", name, n.ID)
		}
		assert.Contains(t, card, "<h3", n.ID)
		assert.Contains(t, card, `data-card-open`, n.ID)
		assert.Contains(t, card, `aria-haspopup="dialog"`, n.ID)
		assert.Contains(t, card, "追加ノード・1 MiB", n.ID)
		assert.Contains(t, card, `data-testid="mm-node-badge"`, n.ID)
		assert.Contains(t, card, `data-testid="mm-node-models"`, "the models that use the node stay on the card: %s", n.ID)
		if n.Blocked != "" {
			assert.NotContains(t, card, `data-testid="mm-node-blocked"`, "the long reason is not on the card (#1118): %s", n.ID)
			assert.NotContains(t, card, html.EscapeString(n.Blocked), n.ID)
			assert.Regexp(t, `<button[^>]*data-testid="mm-reason-open"[^>]*>導入</button>`, card, "the install button opens the reason: %s", n.ID)
			assert.NotRegexp(t, `<button[^>]*\sdisabled[\s>=][^>]*>導入</button>`, card, "and can be pressed: %s", n.ID)
			assert.NotContains(t, card, `hx-post=`, n.ID)
			assert.NotContains(t, card, `data-testid="mm-node-install"`, n.ID)
		} else {
			assert.Contains(t, card, `data-testid="mm-node-install"`, n.ID)
			assert.Contains(t, card, `aria-label="導入: `+n.Name+`"`, n.ID)
		}
	}
}

// The state note stays on the card, and the modal of every node of the allowlist has the description as the one `#mam-modal-desc`.
func TestNodeDetail_DescribesTheDialogWithTheDescription(t *testing.T) {
	for _, c := range nodecat.Default().Nodes {
		body := render(t, nodecard.NodeDetail(nodecard.Node{ID: c.ID, Name: c.Name, Description: c.Description}))
		assert.Contains(t, body, `<p id="mam-modal-desc" class="text-sm text-mute">`+html.EscapeString(c.Description)+`</p>`, c.ID)
	}
	n := nodecard.Node{ID: "n", Name: "N", State: "outdated", Detail: "更新が必要です"}
	assert.Contains(t, render(t, nodecard.NodeCard(n)), `data-testid="mm-node-detail-note"`)
}
