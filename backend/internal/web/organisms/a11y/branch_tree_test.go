package a11y_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
)

func TestBranchTree_NestsABranchUnderTheBranchItStartsFromAndMarksTheSelectedOne(t *testing.T) {
	child := organisms.BranchTreeNode{Branch: domain.Branch{ID: "child", Name: "子", PointType: domain.BranchAtScene}}
	root := organisms.BranchTreeNode{Branch: domain.Branch{ID: "root", Name: "親", PointType: domain.BranchAtScene}, Children: []organisms.BranchTreeNode{child}}

	var sb strings.Builder
	require.NoError(t, organisms.BranchTree(organisms.BranchTreeData{Roots: []organisms.BranchTreeNode{root}, SelectedID: "child"}, false).Render(context.Background(), &sb))
	html := sb.String()

	assert.Regexp(t, `data-branch="root"[^<]*</a>\s*<ul[^>]*>.*data-branch="child"`, html, "the child hangs in a list inside its parent")
	assert.Equal(t, 1, strings.Count(html, `aria-current="true"`))
	assert.Regexp(t, `<a[^>]*data-branch="child"[^>]*aria-current="true"`, html, "only the selected link is the current one")
	assert.NotContains(t, html, "hx-swap-oob")
	assert.NotContains(t, html, "data-empty")
}

func TestBranchTree_SaysSoWhenThereIsNoBranch(t *testing.T) {
	var sb strings.Builder
	require.NoError(t, organisms.BranchTree(organisms.BranchTreeData{}, false).Render(context.Background(), &sb))
	assert.Contains(t, sb.String(), "data-empty")
}

func TestBranchTree_EscapesAnImportedBranchIDInItsLink(t *testing.T) {
	root := organisms.BranchTreeNode{Branch: domain.Branch{ID: "b?1#x%", Name: "親", PointType: domain.BranchAtScene}}

	var sb strings.Builder
	require.NoError(t, organisms.BranchTree(organisms.BranchTreeData{Roots: []organisms.BranchTreeNode{root}}, false).Render(context.Background(), &sb))

	assert.Contains(t, sb.String(), `href="/branches/b%3F1%23x%25"`)
	assert.Contains(t, sb.String(), `hx-get="/branches/b%3F1%23x%25"`)
}

// The tree is a nested list of links reached with Tab: a tree role would promise arrow-key navigation
// that is not there, and aria-selected / aria-expanded would state what nothing can do (#782).
func TestBranchTree_IsAPlainNestedListWithoutTreeRoles(t *testing.T) {
	child := organisms.BranchTreeNode{Branch: domain.Branch{ID: "child", Name: "子", PointType: domain.BranchAtScene}}
	root := organisms.BranchTreeNode{Branch: domain.Branch{ID: "root", Name: "親", PointType: domain.BranchAtScene}, Children: []organisms.BranchTreeNode{child}}

	html := renderHTML(t, organisms.BranchTree(organisms.BranchTreeData{Roots: []organisms.BranchTreeNode{root}, SelectedID: "child"}, false))

	for _, attr := range []string{`role="tree"`, `role="treeitem"`, `role="group"`, "aria-selected", "aria-expanded"} {
		assert.NotContains(t, html, attr)
	}
	assert.Contains(t, html, "<ul")
	assert.Equal(t, 1, strings.Count(html, "aria-current"), "no link but the selected one is current")
}

// The section around the tree must not repeat the name of the navigation inside it: a region and a
// navigation with the same name are announced twice (#782).
func TestBranchView_TreeLandmarkIsNamedOnce(t *testing.T) {
	html := renderHTML(t, organisms.BranchView(organisms.BranchPageData{Project: domain.Project{ID: "p1"}}))

	assert.Equal(t, 1, strings.Count(html, `aria-label="Branch ツリー"`))
	assert.Regexp(t, `<nav[^>]*aria-label="Branch ツリー"`, html)
}
