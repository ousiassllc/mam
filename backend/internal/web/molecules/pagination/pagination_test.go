package pagination_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/molecules/pagination"
)

func href(page int) string {
	if page == 1 {
		return "/list?model=x"
	}
	return fmt.Sprintf("/list?model=x&page=%d", page)
}

func render(t *testing.T, current, last, total int) string {
	t.Helper()
	var sb strings.Builder
	p := pagination.Props{ID: "pager", Target: "list", Current: current, Last: last, Total: total, PerPage: 24, Href: href}
	require.NoError(t, pagination.Nav(p).Render(context.Background(), &sb))
	return sb.String()
}

// The pages shown are the first, the last and the two on each side of the current one; a run that is left out is one gap, and a gap of a single page is
// the page itself (no "…" that hides one number).
func TestWindow_ShowsTheEndsAndTheNeighboursWithGapsBetween(t *testing.T) {
	g := pagination.Gap
	tests := []struct {
		current, last int
		want          []int
	}{
		{1, 1, []int{1}},
		{1, 5, []int{1, 2, 3, 4, 5}},
		{1, 7, []int{1, 2, 3, g, 7}},
		{4, 9, []int{1, 2, 3, 4, 5, 6, g, 9}},
		{5, 9, []int{1, 2, 3, 4, 5, 6, 7, 8, 9}},
		{9, 9, []int{1, g, 7, 8, 9}},
		{6, 20, []int{1, g, 4, 5, 6, 7, 8, g, 20}},
		{50, 100, []int{1, g, 48, 49, 50, 51, 52, g, 100}},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, pagination.Window(tt.current, tt.last), "page %d of %d", tt.current, tt.last)
	}
}

// What is shown of how much there is, and where: "N 件中 a–b 件（M / L ページ）"; the last page ends at the total, not at a full page.
func TestProps_SummaryNamesTheRangeAndThePage(t *testing.T) {
	p := pagination.Props{Current: 1, Last: 2, Total: 41, PerPage: 24}
	assert.Equal(t, "41 件中 1–24 件（1 / 2 ページ）", p.Summary())
	p.Current = 2
	assert.Equal(t, "41 件中 25–41 件（2 / 2 ページ）", p.Summary())
}

// A list of one page has no navigation at all.
func TestNav_HasNothingForASinglePage(t *testing.T) {
	assert.Empty(t, strings.TrimSpace(render(t, 1, 1, 10)))
	assert.Empty(t, strings.TrimSpace(render(t, 1, 1, 0)))
}

// The navigation is named, the current page is marked with aria-current="page" and is not a link, and every other page is a real link (a plain href,
// so it works without a script, and the keyboard reaches it in order) that htmx reads into the target and pushes into the history.
func TestNav_IsANamedNavigationWhoseCurrentPageIsMarkedAndTheRestAreLinks(t *testing.T) {
	html := render(t, 3, 6, 130)

	assert.Contains(t, html, `<nav aria-label="ページ移動"`)
	assert.Equal(t, 1, strings.Count(html, `aria-current="page"`), "one page is the current one")
	assert.Regexp(t, `<span[^>]*aria-current="page"[^>]*>3</span>`, html, "it is a mark, not a link")
	assert.Contains(t, html, `data-testid="pager-summary"`)
	assert.Contains(t, html, "130 件中 49–72 件（3 / 6 ページ）")
	for _, want := range []string{
		`id="pager-first" href="/list?model=x"`,
		`id="pager-prev" href="/list?model=x&amp;page=2"`,
		`id="pager-next" href="/list?model=x&amp;page=4"`,
		`id="pager-last" href="/list?model=x&amp;page=6"`,
		`id="pager-5" href="/list?model=x&amp;page=5" class=`,
		`aria-label="5 ページ目"`,
	} {
		assert.Contains(t, html, want)
	}
	assert.Contains(t, html, `hx-target="#list"`)
	assert.Contains(t, html, `hx-push-url="true"`, "back, forward, a bookmark and a reload come to the same page")
	assert.Contains(t, html, `hx-swap="outerHTML show:#list:top"`, "the page that comes is read from its top")
	assert.NotContains(t, html, `id="pager-3"`, "the current page has no link")
}

// On the first page there is no way back and on the last none forward: the words stay, dimmed, and are not links (the keyboard skips them).
func TestNav_DimsTheStepsThatHaveNowhereToGo(t *testing.T) {
	first := render(t, 1, 3, 60)
	assert.NotContains(t, first, `id="pager-first"`)
	assert.NotContains(t, first, `id="pager-prev"`)
	assert.Contains(t, first, `aria-disabled="true">前へ</span>`)
	assert.Contains(t, first, `id="pager-next"`)

	last := render(t, 3, 3, 60)
	assert.NotContains(t, last, `id="pager-next"`)
	assert.NotContains(t, last, `id="pager-last"`)
	assert.Contains(t, last, `aria-disabled="true">次へ</span>`)
	assert.Contains(t, last, `id="pager-prev"`)
}

// A long list leaves pages out with a mark that is not read aloud.
func TestNav_LeavesPagesOutWithAHiddenGap(t *testing.T) {
	html := render(t, 50, 100, 2400)
	assert.Equal(t, 2, strings.Count(html, `aria-hidden="true">…</li>`))
	assert.Equal(t, 1, strings.Count(html, `id="pager-100"`))
	assert.Equal(t, 0, strings.Count(html, `id="pager-10"`))
}
