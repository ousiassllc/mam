package screentest_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

func TestHistoryListsNewestFirstFiltersAndShowsTheSpend(t *testing.T) {
	s := newScreen(t)
	s.run(batchForm("flux-2"))
	s.run(url.Values{"media": {"image"}, "model": {"nano-banana"}, "prompt": {"a green fox"}, "negative": {""}})

	full := s.page(paths.GenerationTestHistory())
	assert.Equal(t, 5, strings.Count(full, `data-testid="gt-history-item"`))
	assert.Contains(t, full, `data-testid="gt-spend"`)
	assert.Contains(t, full, "これまでの単発生成の合計: $0.1536", "4 x 0.03 + 1 x 0.0336")
	assert.Contains(t, full, "<html", "a direct visit is the whole page")
	assert.Less(t, strings.Index(full, "a green fox"), strings.Index(full, "a red fox"), "newest first")

	only := s.get(paths.GenerationTestHistory() + "?model=nano-banana")
	assert.Equal(t, 1, strings.Count(only.Body.String(), `data-testid="gt-history-item"`))
	assert.NotContains(t, only.Body.String(), "<html", "an HTMX request gets the list alone")
	assert.Contains(t, only.Body.String(), `<option value="nano-banana" selected`, "the filter shows what it filters by")
	none := s.get(paths.GenerationTestHistory() + "?favorite=1").Body.String()
	assert.Contains(t, none, `data-testid="gt-history-empty"`)
	assert.Equal(t, http.StatusUnprocessableEntity, s.get(paths.GenerationTestHistory()+"?media=audio").Code)
}

// twoPagesOf41 runs 40 results of flux-2 and 1 of nano-banana (newest): 24 on the first page and 17 on the second, 41 in all, by 24 a page.
func twoPagesOf41(t *testing.T) *screen {
	t.Helper()
	s := newScreen(t)
	form := url.Values{"media": {"image"}, "model": {"flux-2"}, "prompt": {"fox"}, "negative": {""}, "count": {"20"}}
	s.run(form)
	s.run(form)
	s.run(url.Values{"media": {"image"}, "model": {"nano-banana"}, "prompt": {"cat"}, "negative": {""}})
	return s
}

func items(body string) int { return strings.Count(body, `data-testid="gt-history-item"`) }

// #1079: the history is paged (24 a page): the navigation says where the page is, a link goes to a page by its address (so back, forward, a bookmark and a
// reload come to the same page), the filter is kept in the links, and "さらに表示" and the offset are gone.
func TestHistoryPagesWithANavigationThatKeepsTheFilter(t *testing.T) {
	s := twoPagesOf41(t)
	all := s.get(paths.GenerationTestHistory()).Body.String()
	assert.Equal(t, 24, items(all))
	assert.Contains(t, all, `<nav aria-label="ページ移動"`)
	assert.Contains(t, all, "41 件中 1–24 件（1 / 2 ページ）")
	assert.NotContains(t, all, `data-testid="gt-more"`, "no more 'さらに表示'")
	assert.NotContains(t, all, "offset=")
	assert.Regexp(t, `<span[^>]*aria-current="page"[^>]*>1</span>`, all, "the page that is shown is marked, and it is not a link")

	flux := s.get(paths.GenerationTestHistory() + "?model=flux-2").Body.String()
	assert.Equal(t, 24, items(flux))
	assert.Contains(t, flux, "40 件中 1–24 件（1 / 2 ページ）", "the count is of the filtered list")
	next := regexp.MustCompile(`id="gt-pager-next" href="([^"]*)"`).FindStringSubmatch(flux)
	require.Len(t, next, 2, "a next page")
	assert.Equal(t, "/generation-test/history?model=flux-2&amp;page=2", next[1], "the filter is in the link of the page, and the page is a page, not an offset")
	assert.Contains(t, flux, `hx-push-url="true"`, "a page goes into the history of the browser")
	assert.Contains(t, flux, `hx-get="/generation-test/history?model=flux-2&amp;page=2"`)

	rest := s.get(strings.ReplaceAll(next[1], "&amp;", "&")).Body.String()
	assert.Equal(t, 16, items(rest), "the second page has the rest, not the first 24 again")
	assert.Contains(t, rest, "40 件中 25–40 件（2 / 2 ページ）")
	assert.Regexp(t, `<span[^>]*aria-current="page"[^>]*>2</span>`, rest)
	assert.Regexp(t, `id="gt-pager-prev" href="/generation-test/history\?model=flux-2"`, rest, "back to the first page: its address has no page")
	assert.NotContains(t, rest, `id="gt-pager-next"`, "no next on the last page")

	assert.Equal(t, 17, items(s.page(paths.GenerationTestHistory()+"?page=2")), "the address of a page opens that page as a whole page, for a bookmark and a reload")
}

// 24 results or fewer are one page: no navigation.
func TestHistoryOfOnePageHasNoNavigation(t *testing.T) {
	s := newScreen(t)
	s.run(batchForm("flux-2"))
	assert.NotContains(t, s.get(paths.GenerationTestHistory()).Body.String(), `aria-label="ページ移動"`)
	assert.NotContains(t, s.get(paths.GenerationTestHistory()+"?page=1&model=nope").Body.String(), `aria-label="ページ移動"`, "nor does an empty list")
}

// A page that is not a page never breaks the screen: not a number and below 1 are the first page, past the last one is the last page (so a delete that
// emptied the last page lands on the one before it).
func TestHistoryRoundsAPageThatDoesNotExist(t *testing.T) {
	s := twoPagesOf41(t)
	first := s.get(paths.GenerationTestHistory()).Body.String()
	for _, bad := range []string{"0", "-3", "abc", "", "1.5", "99999999999999999999"} {
		rec := s.get(paths.GenerationTestHistory() + "?page=" + url.QueryEscape(bad))
		require.Equal(t, http.StatusOK, rec.Code, bad)
		assert.Equal(t, 24, items(rec.Body.String()), "page=%q is the first page", bad)
		assert.Contains(t, rec.Body.String(), "（1 / 2 ページ）", bad)
	}
	assert.Contains(t, first, "（1 / 2 ページ）")
	last := s.get(paths.GenerationTestHistory() + "?page=999")
	require.Equal(t, http.StatusOK, last.Code)
	assert.Equal(t, 17, items(last.Body.String()), "past the last page is the last page")
	assert.Contains(t, last.Body.String(), "（2 / 2 ページ）")
}

// Deleting every result of the last page takes the reader who is on it to the page before (it is rounded, not an empty list).
func TestHistoryLastPageThatWasEmptiedShowsThePageBefore(t *testing.T) {
	s := twoPagesOf41(t)
	second := s.get(paths.GenerationTestHistory() + "?page=2").Body.String()
	ids := regexp.MustCompile(`data-result="([^"]+)"`).FindAllStringSubmatch(second, -1)
	require.Len(t, ids, 17)
	for _, m := range ids {
		require.Equal(t, http.StatusOK, s.do(http.MethodDelete, paths.GenerationTestPart("results", m[1]), nil).Code)
	}
	body := s.get(paths.GenerationTestHistory() + "?page=2").Body.String()
	assert.Equal(t, 24, items(body), "the page before")
	assert.NotContains(t, body, `aria-label="ページ移動"`, "24 results are one page now")
}

// A filter that leaves one page shows it whatever page was asked for, and the filter does not need the first page to be in the address.
func TestHistoryFilterThatLeavesOnePageRoundsThePageAsked(t *testing.T) {
	s := twoPagesOf41(t)
	rec := s.get(paths.GenerationTestHistory() + "?model=nano-banana&page=7")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, 1, items(rec.Body.String()))
	assert.NotContains(t, rec.Body.String(), `aria-label="ページ移動"`)
}

func TestFavoriteTogglesAndFiltersTheHistory(t *testing.T) {
	s := newScreen(t)
	id := s.run(batchForm("flux-2"))
	first := s.results(id)[0].ID
	on := s.do(http.MethodPost, paths.GenerationTestPart("results", first, "favorite"), url.Values{"favorite": {"1"}})
	require.Equal(t, http.StatusOK, on.Code)
	assert.Contains(t, on.Body.String(), `data-on="true"`)
	assert.Contains(t, on.Body.String(), "お気に入りを外す")
	list := s.get(paths.GenerationTestHistory() + "?favorite=1").Body.String()
	assert.Equal(t, 1, strings.Count(list, `data-testid="gt-history-item"`))
	off := s.do(http.MethodPost, paths.GenerationTestPart("results", first, "favorite"), url.Values{"favorite": {"0"}})
	assert.Contains(t, off.Body.String(), `data-on="false"`)
	assert.Equal(t, http.StatusNotFound, s.do(http.MethodPost, paths.GenerationTestPart("results", "nope", "favorite"), url.Values{"favorite": {"1"}}).Code)
}

func TestResultFileIsServedInASandboxAndAsADownloadOnRequest(t *testing.T) {
	s := newScreen(t)
	first := s.results(s.run(batchForm("flux-2")))[0].ID
	file := s.get(paths.GenerationTestPart("results", first, "file"))
	require.Equal(t, http.StatusOK, file.Code)
	assert.Equal(t, "image/png", file.Header().Get("Content-Type"))
	assert.Equal(t, "sandbox", file.Header().Get("Content-Security-Policy"))
	assert.Equal(t, gentesttest.PNG, file.Body.Bytes())
	assert.Empty(t, file.Header().Get("Content-Disposition"))
	dl := s.get(paths.GenerationTestPart("results", first, "file") + "?download=1")
	assert.Contains(t, dl.Header().Get("Content-Disposition"), "attachment")
	assert.Equal(t, http.StatusNotFound, s.get(paths.GenerationTestPart("results", "nope", "file")).Code)
	assert.Equal(t, http.StatusNotFound, s.get(paths.GenerationTestPart("inputs", "nope", "file")).Code)
}

func TestDetailShowsWhatWasSentAndTheInputsAndDeleteGoesBackToTheHistory(t *testing.T) {
	s := newScreen(t)
	staged := s.upload(paths.GenerationTestPart("estimate"), batchForm("flux-2"), "upload", gentesttest.PNG)
	require.Equal(t, http.StatusOK, staged.Code)
	_, id := s.start(hiddenOf(t, staged.Body.String()))
	s.settled(id)
	var red string
	for _, r := range s.results(id) {
		if r.NegativePrompt == "blurry" {
			red = r.ID
		}
	}
	body := s.page(paths.GenerationTestPart("results", red))
	assert.Contains(t, body, `data-testid="gt-sent-prompt"`)
	assert.Contains(t, body, "a red fox")
	assert.Contains(t, body, "ネガティブ（プロンプトに書き足した）", "flux has no negative field: it was folded")
	assert.Contains(t, body, "シード")
	assert.Contains(t, body, `data-testid="gt-inputs-list"`, "the input image is shown")
	inputURL := regexp.MustCompile(`src="(/generation-test/inputs/[^"]+/file)"`).FindStringSubmatch(body)
	require.Len(t, inputURL, 2)
	in := s.get(inputURL[1])
	require.Equal(t, http.StatusOK, in.Code)
	assert.Equal(t, gentesttest.PNG, in.Body.Bytes())
	assert.Contains(t, body, paths.GenerationTest()+"?ref=history::"+red, "the result can be an input of the next batch")

	del := s.do(http.MethodDelete, paths.GenerationTestPart("results", red), nil)
	require.Equal(t, http.StatusOK, del.Code)
	assert.Equal(t, paths.GenerationTestHistory(), del.Header().Get("HX-Redirect"))
	assert.Equal(t, http.StatusNotFound, s.get(paths.GenerationTestPart("results", red, "file")).Code)
	req := httptest.NewRequest(http.MethodGet, paths.GenerationTestPart("results", red), nil)
	assert.Equal(t, http.StatusNotFound, s.send(req).Code)
	assert.Equal(t, http.StatusNotFound, s.do(http.MethodDelete, paths.GenerationTestPart("results", red), nil).Code)
}

func TestPromptHistoryIsDeletedOnlyOnPurposeAndNotWithTheResults(t *testing.T) {
	s := newScreen(t)
	id := s.run(batchForm("flux-2"))
	page := s.page(paths.GenerationTestPrompts())
	assert.Equal(t, 2, strings.Count(page, `data-testid="gt-prompt-item"`))
	assert.Contains(t, page, "1 回使用", "once per batch, however many results it made")

	for _, r := range s.results(id) {
		require.NoError(t, s.rig.Svc.DeleteResult(t.Context(), r.ID))
	}
	assert.Equal(t, 2, strings.Count(s.page(paths.GenerationTestPrompts()), `data-testid="gt-prompt-item"`), "deleting every result keeps the prompts")

	prompts, err := s.rig.Svc.Prompts(t.Context())
	require.NoError(t, err)
	del := s.do(http.MethodDelete, paths.GenerationTestPart("prompts", prompts[0].ID), nil)
	require.Equal(t, http.StatusOK, del.Code)
	assert.Equal(t, 1, strings.Count(del.Body.String(), `data-testid="gt-prompt-item"`))
	assert.Equal(t, http.StatusNotFound, s.do(http.MethodDelete, paths.GenerationTestPart("prompts", prompts[0].ID), nil).Code)
	assert.Contains(t, s.page(paths.GenerationTestPrompts()), "/generation-test?prompt=")
}
