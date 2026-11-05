package screentest_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// inModal asks for the detail of a result as the tile of a grid does: through htmx, into the body of the modal.
func (s *screen) inModal(method, path string, form url.Values) *httptest.ResponseRecorder {
	s.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Target", "mam-modal-body")
	return s.htmx(req)
}

// #1004: a tile opens the detail in the modal. It is the same detail as the page of the result: everything a card showed (the model, the state, the time, the
// whole prompt, the settings, the picture) and could do (the favorite, the download, the delete, the next steps).
func TestATileOpensTheDetailInTheModalWithEverythingTheCardHad(t *testing.T) {
	s := newScreen(t, gentesttest.WithUpscaleModels())
	id := s.results(s.run(batchForm("flux-2")))[0].ID
	path := paths.GenerationTestPart("results", id)

	rec := s.inModal(http.MethodGet, path, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.NotContains(t, body, "<html", "the body of the dialog, not a page")
	assert.Contains(t, body, `id="mam-modal-title"`, "the dialog is named by its heading")
	assert.Contains(t, body, `data-modal-wide`, "and asks for the wide dialog")
	assert.Contains(t, rec.Header().Get("Vary"), "HX-Target", "the answer differs by what asked")
	for _, want := range []string{
		`data-testid="gt-card-status"`, `data-testid="gt-detail-time"`, `data-testid="gt-sent-prompt"`, `data-testid="gt-params"`,
		`data-testid="gt-favorite"`, `data-testid="gt-download"`, `data-testid="gt-delete"`, `data-testid="gt-use"`, `data-testid="gt-match-link"`,
		`data-testid="gt-next"`, `data-testid="gt-used"`, `data-testid="gt-upscale"`, `data-testid="commercial-badge"`, "a red fox", paths.GenerationTestPart("results", id, "file"),
	} {
		assert.Contains(t, body, want)
	}
	assert.NotContains(t, body, `data-testid="gt-modal-pick"`, "no choice for the export unless the grid has it")

	page := s.page(path)
	assert.Contains(t, page, "<html", "anything else gets the page")
	assert.Contains(t, page, `data-testid="gt-delete"`)
	assert.NotContains(t, page, "data-modal-wide")

	picked := s.inModal(http.MethodGet, path+"?pick=1", nil).Body.String()
	assert.Contains(t, picked, `data-gt-modal-pick="`+id+`"`, "the history's tiles ask for it, and the modal offers the choice")

	gone := s.inModal(http.MethodGet, paths.GenerationTestPart("results", "nope"), nil)
	assert.Equal(t, http.StatusNotFound, gone.Code)
	assert.Contains(t, gone.Body.String(), "mam-modal-error", "a result that is gone says so in the modal, with a way to close it")
	assert.Contains(t, gone.Body.String(), "data-modal-close")
}

// 失敗した結果もモーダルで見られる: 状態のバッジと失敗の理由が出て、ファイルが無いことが書いてある。
func TestAFailedResultsDetailInTheModalSaysWhyAndThatThereIsNoFile(t *testing.T) {
	s := newScreen(t)
	s.rig.Fake.FailWith("flux-2", assert.AnError)
	id := s.results(s.run(batchForm("flux-2")))[0].ID

	body := s.inModal(http.MethodGet, paths.GenerationTestPart("results", id), nil).Body.String()
	assert.Contains(t, body, `data-testid="gt-card-status"`)
	assert.Contains(t, body, `data-testid="gt-card-error"`)
	assert.Contains(t, body, `data-testid="error-box"`, "the failure is the common error box")
	assert.Contains(t, body, `data-error-code="unknown"`)
	assert.Contains(t, body, "原因がわかりませんでした")
	assert.Contains(t, body, `data-testid="error-copy"`)
	assert.Contains(t, body, `data-testid="gt-no-file"`)
	assert.NotContains(t, body, `data-testid="gt-download"`)
}

// モーダルのお気に入りは、ボタンとグリッドのタイルのハートを一緒に返す（out of band）。
func TestTheFavoriteOfTheModalAnswersTheButtonAndTheHeartOfTheTile(t *testing.T) {
	s := newScreen(t)
	id := s.results(s.run(batchForm("flux-2")))[0].ID

	rec := s.inModal(http.MethodPost, paths.GenerationTestPart("results", id, "favorite"), url.Values{"favorite": {"1"}})
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, `data-on="true"`, "the button")
	assert.Contains(t, body, `id="gt-fav-mark-`+id+`"`)
	assert.Contains(t, body, `hx-swap-oob="true"`, "the heart of the tile is swapped out of band")
	assert.NotContains(t, body, `id="gt-fav-mark-`+id+`" class="pointer-events-none absolute right-1.5 top-1.5 text-base leading-none text-warning drop-shadow" hidden`, "a favorite shows the heart")

	history := s.page(paths.GenerationTestHistory())
	assert.Contains(t, history, `id="gt-fav-mark-`+id+`"`)
	off := s.inModal(http.MethodPost, paths.GenerationTestPart("results", id, "favorite"), url.Values{"favorite": {"0"}}).Body.String()
	assert.Contains(t, off, `data-on="false"`)
	assert.Contains(t, off, `hidden`, "no longer a favorite: the heart is hidden")
}

// モーダルから消すと、グリッドの要素が out of band で外れ、モーダルが閉じる。ページから消すと履歴へ戻る（今まで通り）。
func TestDeletingInTheModalTakesTheTileOutOfTheGridAndClosesIt(t *testing.T) {
	s := newScreen(t)
	results := s.results(s.run(batchForm("flux-2")))
	id := results[0].ID

	rec := s.inModal(http.MethodDelete, paths.GenerationTestPart("results", id), nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Header().Get("HX-Redirect"), "the page behind the modal stays: nothing is lost (the choice for the export, the place in the list)")
	assert.Equal(t, "modal-close", rec.Header().Get("HX-Trigger-After-Swap"))
	assert.Contains(t, rec.Body.String(), `id="gt-result-`+id+`" hx-swap-oob="delete"`)
	assert.NotContains(t, s.page(paths.GenerationTestHistory()), `data-result="`+id+`"`)
	assert.Contains(t, s.page(paths.GenerationTestHistory()), `data-result="`+results[1].ID+`"`)
}

// 消せない結果（まだ動いている）は、詳細を消さずに理由を上に出す。モーダルでも同じ。
func TestARefusedDeleteKeepsTheDetailAndSaysWhy(t *testing.T) {
	s := newScreen(t)
	release := s.rig.Fake.Hold("flux-2")
	_, batch := s.start(s.confirmed(batchForm("flux-2")))
	<-s.rig.Fake.Started() // a call of the model has begun: its result runs
	t.Cleanup(func() { close(release); s.rig.Wait(batch) })
	var running string
	for _, r := range s.results(batch) {
		if r.Status == "running" {
			running = r.ID
		}
	}
	require.NotEmpty(t, running)

	for _, inModal := range []bool{true, false} {
		var rec *httptest.ResponseRecorder
		if inModal {
			rec = s.inModal(http.MethodDelete, paths.GenerationTestPart("results", running), nil)
		} else {
			rec = s.do(http.MethodDelete, paths.GenerationTestPart("results", running), nil)
		}
		assert.Equal(t, http.StatusConflict, rec.Code)
		assert.Equal(t, "#gt-detail", rec.Header().Get("HX-Retarget"))
		assert.Contains(t, rec.Body.String(), `data-testid="gt-detail-error"`, "the reason")
		assert.Contains(t, rec.Body.String(), `data-testid="gt-sent-prompt"`, "and the detail is not wiped")
		assert.Equal(t, inModal, strings.Contains(rec.Body.String(), "data-modal-wide"))
	}
}
