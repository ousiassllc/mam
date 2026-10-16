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

	assert.Regexp(t, `data-branch="root"[^<]*</a>\s*<ul role="group"[^>]*>.*data-branch="child"`, html, "the child hangs in a group inside its parent")
	assert.Equal(t, 1, strings.Count(html, `aria-selected="true"`))
	assert.Regexp(t, `aria-selected="true"[^>]*>\s*<a[^>]*data-branch="child"`, html)
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
