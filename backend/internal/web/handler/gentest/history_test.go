package gentest_test

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
	assert.Contains(t, full, "これまでの生成テストの合計: $0.1600", "4 x 0.03 + 1 x 0.04")
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

func TestHistoryPagesAndKeepsTheFilterInTheNextLink(t *testing.T) {
	s := newScreen(t)
	form := url.Values{"media": {"image"}, "model": {"flux-2"}, "prompt": {"fox"}, "negative": {""}, "count": {"20"}}
	s.run(form)
	s.run(form)
	body := s.get(paths.GenerationTestHistory() + "?model=flux-2").Body.String()
	assert.Equal(t, 24, strings.Count(body, `data-testid="gt-history-item"`))
	next := regexp.MustCompile(`hx-get="([^"]*offset=24[^"]*)"`).FindStringSubmatch(body)
	require.Len(t, next, 2, "a next page")
	assert.Contains(t, next[1], "model=flux-2")
	rest := s.get(strings.ReplaceAll(next[1], "&amp;", "&")).Body.String()
	assert.Equal(t, 16, strings.Count(rest, `data-testid="gt-history-item"`))
	assert.NotContains(t, rest, `data-testid="gt-more"`)
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
	run := s.do(http.MethodPost, paths.GenerationTestPart("run"), hiddenOf(t, staged.Body.String()))
	id := batchID(t, run.Body.String())
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
