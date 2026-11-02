package exportscreen_test

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/router/settingsroutes"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/pick"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.ReleaseMode)
	m.Run()
}

type screen struct {
	t      *testing.T
	rig    *gentesttest.Rig
	engine http.Handler
}

func newScreen(t *testing.T) *screen {
	t.Helper()
	rig := gentesttest.New(t)
	svc := servicetest.New(t, service.Deps{DB: rig.DB, Credentials: credentialtest.New()})
	settings := settingsroutes.New(t.TempDir()+"/llm.json", false, t.TempDir()+"/m.json", false)
	settings.Gentest = rig.Svc
	return &screen{t: t, rig: rig, engine: router.New(svc, router.Options{Settings: settings})}
}

func (s *screen) get(path string, header ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for i := 0; i < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	req.Host, req.RemoteAddr = "127.0.0.1:8787", "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	s.engine.ServeHTTP(rec, req)
	return rec
}

// images makes the results of one batch (one per prompt) with the model and returns them.
func (s *screen) images(model string, prompts ...string) []record.ResultView {
	s.t.Helper()
	req := gentest.Request{Media: "image", Models: []string{model}}
	for _, p := range prompts {
		req.Prompts = append(req.Prompts, gentest.PromptInput{Prompt: p})
	}
	ref, err := s.rig.Confirmed(req)
	require.NoError(s.t, err)
	return s.rig.Wait(ref.BatchID).Results
}

func ask(kv ...string) string {
	q := url.Values{}
	for i := 0; i < len(kv); i += 2 {
		q.Add(kv[i], kv[i+1])
	}
	return paths.GenerationTestExport() + "?" + q.Encode()
}

// sheet opens the ZIP and returns the rows of prompts.csv (without the header) and the names of the files.
func sheet(t *testing.T, rec *httptest.ResponseRecorder) (rows [][]string, files map[string][]byte) {
	t.Helper()
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	require.NoError(t, err)
	files = map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		require.NoError(t, err)
		data, err := io.ReadAll(rc)
		require.NoError(t, err)
		_ = rc.Close()
		files[f.Name] = data
	}
	raw := files["prompts/prompts.csv"]
	require.True(t, bytes.HasPrefix(raw, []byte("\xEF\xBB\xBF")), "BOM")
	all, err := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(raw, []byte("\xEF\xBB\xBF")))).ReadAll()
	require.NoError(t, err)
	return all[1:], files
}

func TestExportAnswersTheChosenResultsAsAZip(t *testing.T) {
	s := newScreen(t)
	res := s.images("flux-2", "a red fox", "a blue fox", "a green fox")

	rec := s.get(ask("id", res[0].ID, "id", res[2].ID))
	assert.Equal(t, "application/zip", rec.Header().Get("Content-Type"))
	assert.Regexp(t, `^attachment; filename=generation-results_\d{8}-\d{6}_2\.zip$`, rec.Header().Get("Content-Disposition"))
	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	rows, files := sheet(t, rec)
	require.Len(t, rows, 2)
	got := []string{rows[0][7], rows[1][7]}
	assert.ElementsMatch(t, []string{res[0].ID, res[2].ID}, got, "only what was chosen")
	for _, row := range rows {
		assert.Equal(t, gentesttest.PNG, files["photos/"+row[0]])
	}
	assert.Len(t, files, 3)
}

func TestExportTakesTheNewestNOfTheFilterOfTheHistory(t *testing.T) {
	s := newScreen(t)
	s.images("flux-2", "one", "two", "three")
	banana := s.images("nano-banana", "four")
	require.NoError(t, s.rig.Svc.SetFavorite(t.Context(), banana[0].ID, true))

	count := func(kv ...string) int {
		rows, _ := sheet(t, s.get(ask(kv...)))
		return len(rows)
	}
	assert.Equal(t, 2, count("latest", "2"))
	assert.Equal(t, 4, count(), "no number: the newest 40")
	assert.Equal(t, 1, count("batch", banana[0].BatchID))
	assert.Equal(t, 3, count("model", "flux-2", "latest", "40"))
	assert.Equal(t, 1, count("favorite", "1"))
	assert.Equal(t, 4, count("media", "image", "commercial", "ok"))
}

func TestExportRefusesWhatCannotBeExportedAsText(t *testing.T) {
	s := newScreen(t)
	res := s.images("flux-2", "fox")
	for name, path := range map[string]string{
		"nothing exists": ask("id", "does-not-exist"),
		"bad number":     ask("latest", "many"),
		"too many":       ask("latest", "501"),
		"bad media":      ask("media", "audio"),
		"bad filter":     ask("commercial", "maybe"),
		"bad id":         ask("id", "../../etc/passwd"),
		"no such batch":  ask("batch", "nope"),
	} {
		rec := s.get(path)
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, name)
		assert.Contains(t, rec.Header().Get("Content-Type"), "text/plain", name)
		assert.NotContains(t, rec.Body.String(), "PK", name, "no ZIP")
		assert.NotEmpty(t, strings.TrimSpace(rec.Body.String()), name)
	}
	assert.Equal(t, http.StatusOK, s.get(ask("id", res[0].ID)).Code)
}

func TestHistoryHasTheExportBarAndOffersOnlyResultsWithAFile(t *testing.T) {
	s := newScreen(t)
	res := s.images("flux-2", "fox", "cat")
	release := s.rig.Fake.Hold(domain.ModelNanoBanana)
	t.Cleanup(func() { close(release) })
	_, err := s.rig.Confirmed(gentest.Request{Media: "image", Models: []string{"nano-banana"}, Prompts: []gentest.PromptInput{{Prompt: "dog"}}})
	require.NoError(t, err)
	<-s.rig.Fake.Started()

	body := s.get(paths.GenerationTestHistory() + "?model=flux-2&media=image&favorite=1").Body.String()
	assert.Contains(t, body, `data-testid="gt-export"`)
	assert.Contains(t, body, `action="/generation-test/export"`)
	assert.Contains(t, body, `name="model" value="flux-2"`, "the export of the filter takes the filter")
	assert.Contains(t, body, `name="media" value="image"`)
	assert.Contains(t, body, `name="favorite" value="1"`)
	assert.Contains(t, body, `name="commercial" value="all"`, "the filter's commercial choice (here the default: all)")
	assert.Contains(t, body, `name="latest"`)
	assert.Contains(t, body, `data-testid="gt-export-selected-submit"`)

	all := s.get(paths.GenerationTestHistory()).Body.String()
	assert.Equal(t, 3, strings.Count(all, `data-testid="gt-history-item"`))
	assert.Equal(t, 2, strings.Count(all, `data-gt-pick="`), "the running result has no file, so it cannot be chosen")
	assert.Equal(t, 3, strings.Count(all, `class="flex flex-col gap-1 rounded-lg border border-transparent p-1.5 `+pick.Glow+`"`), "the frame of every card (also the running one) takes the look of the cards that are chosen: the classes of pick.Glow, not a copy")
	for _, r := range res {
		assert.Contains(t, all, `data-gt-pick="`+r.ID+`"`)
	}
	assert.Equal(t, 2, strings.Count(all, `data-gt-pick-input`), "a checkbox for the keyboard and a screen reader on each")

	live := s.get(paths.GenerationTestHistory()+"?live=1", "HX-Request", "true").Body.String()
	assert.NotContains(t, live, `data-testid="gt-export"`, "the poll never swaps the export bar the reader is using")
	assert.Contains(t, live, `data-gt-pick="`+res[0].ID+`"`, "but its cards are chosen again after the swap")
}

// The bar asks through htmx before it downloads. A choice that cannot be saved is a fragment beside the buttons (an error status that
// main.ts swaps in), never the plain text that would replace the whole screen, where the desktop window has no back button.
func TestExportThroughHtmxSaysWhyItCannotBeSavedInAFragment(t *testing.T) {
	s := newScreen(t)
	s.images("flux-2", "fox")
	for name, tc := range map[string]struct {
		path   string
		status int
		text   string
	}{
		"no result of the filter": {ask("media", "video", "latest", "40"), http.StatusUnprocessableEntity, "書き出せる結果がありません"},
		"nothing exists":          {ask("id", "does-not-exist"), http.StatusUnprocessableEntity, "書き出せる結果がありません"},
		"bad number":              {ask("latest", "many"), http.StatusUnprocessableEntity, "件数は数字で指定してください"},
		"too many":                {ask("latest", "501"), http.StatusUnprocessableEntity, "1〜"},
	} {
		rec := s.get(tc.path, "HX-Request", "true")
		assert.Equal(t, tc.status, rec.Code, name)
		assert.Contains(t, rec.Header().Get("Content-Type"), "text/html", name)
		assert.Empty(t, rec.Header().Get("HX-Redirect"), name+": nothing to download")
		body := rec.Body.String()
		assert.Contains(t, body, `data-testid="gt-export-error"`, name)
		assert.Contains(t, body, `role="alert"`, name)
		assert.Contains(t, body, tc.text, name)
		assert.NotContains(t, body, "<html", name, "a fragment, not a page")
	}
}

// One that can be saved is answered with a redirect to the very same URL, which the browser loads as the download without leaving the
// page; the ZIP itself is only the answer of that second request.
func TestExportThroughHtmxRedirectsToTheDownloadOfTheSameChoice(t *testing.T) {
	s := newScreen(t)
	res := s.images("flux-2", "fox", "cat")
	path := ask("id", res[1].ID)

	rec := s.get(path, "HX-Request", "true")

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, path, rec.Header().Get("HX-Redirect"))
	assert.Empty(t, rec.Body.String(), "no ZIP in the htmx answer")
	assert.Empty(t, rec.Header().Get("Content-Disposition"))
	rows, _ := sheet(t, s.get(rec.Header().Get("HX-Redirect")))
	require.Len(t, rows, 1)
	assert.Equal(t, res[1].ID, rows[0][7])
}

func TestHistoryBarAsksThroughHtmxAndHasALineForTheReason(t *testing.T) {
	s := newScreen(t)
	s.images("flux-2", "fox")

	body := s.get(paths.GenerationTestHistory()).Body.String()

	assert.Equal(t, 2, strings.Count(body, `hx-get="/generation-test/export"`), "both forms ask first")
	assert.Equal(t, 2, strings.Count(body, `hx-target="#gt-export-status"`))
	assert.Equal(t, 2, strings.Count(body, `action="/generation-test/export"`), "and still work as plain forms without the script")
	assert.Contains(t, body, `id="gt-export-status"`)
}
