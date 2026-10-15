// Package swap_test checks which part of the screen an HTMX answer replaces: an action on one row must
// not carry the forms of the other rows, nor lose the filter of the list it was taken from.
package swap_test

import (
	"bytes"
	"context"
	"database/sql"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/testenv"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/notelist"
)

func init() { gin.SetMode(gin.TestMode) }

type app struct {
	t      *testing.T
	db     *repository.DB
	engine *gin.Engine
	cookie *http.Cookie
	token  string
}

// newApp is an app with its own database that does what a browser does: it keeps the CSRF cookie and
// sends the token with every request.
func newApp(t *testing.T) *app {
	t.Helper()
	testenv.Parallel(t)
	d := testdb.Open(t)
	svc := servicetest.New(t, service.Deps{DB: d, Credentials: credentialtest.New(), AssetsDir: t.TempDir()})
	a := &app{t: t, db: d, engine: router.New(svc, router.Options{})}
	rec := a.serve(httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	for _, c := range rec.Result().Cookies() {
		if c.Name == middleware.CSRFCookieName {
			a.cookie = c
		}
	}
	m := regexp.MustCompile(`<meta name="csrf-token" content="([^"]+)"`).FindStringSubmatch(rec.Body.String())
	require.NotNil(t, a.cookie)
	require.Len(t, m, 2)
	a.token = m[1]
	return a
}

func (a *app) serve(req *http.Request) *httptest.ResponseRecorder {
	req.Host, req.RemoteAddr = "127.0.0.1:8787", "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	a.engine.ServeHTTP(rec, req)
	return rec
}

// htmx sends an HTMX request (cookie, CSRF header, HX-Request).
func (a *app) htmx(method, path string, body []byte, contentType string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.AddCookie(a.cookie)
	req.Header.Set(middleware.CSRFHeaderName, a.token)
	req.Header.Set("HX-Request", "true")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return a.serve(req)
}

func (a *app) form(method, path string, v url.Values) *httptest.ResponseRecorder {
	return a.htmx(method, path, []byte(v.Encode()), "application/x-www-form-urlencoded")
}

func (a *app) multipart(path string, fields map[string]string, file string) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		require.NoError(a.t, w.WriteField(k, v))
	}
	if file != "" {
		part, err := w.CreateFormFile("file", file)
		require.NoError(a.t, err)
		_, err = part.Write(append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...))
		require.NoError(a.t, err)
	}
	require.NoError(a.t, w.Close())
	return a.htmx(http.MethodPost, path, buf.Bytes(), w.FormDataContentType())
}

func (a *app) createProject() string {
	rec := a.multipart("/projects", map[string]string{"content": "美容液の縦型15秒CMを作りたい"}, "")
	require.Equal(a.t, http.StatusCreated, rec.Code, rec.Body.String())
	return strings.TrimPrefix(rec.Header().Get("HX-Redirect"), "/projects/")
}

// An action on one note answers with that note in full; the other notes the page has are placeholders
// the page keeps, so the text typed into their forms survives (#694). A note the page does not have
// (added in another tab since) comes in full, not as a placeholder that would stay empty (#697).
func TestNotes_AnActionOnOneNoteDoesNotCarryTheFormsOfTheOthers(t *testing.T) {
	a := newApp(t)
	base := "/projects/" + a.createProject() + "/notes"
	idRe := regexp.MustCompile(`hx-put="/notes/([^"]+)"`)
	rec := a.form(http.MethodPost, base, url.Values{"title": {"口調"}, "content": {"落ち着いた口調"}, "category": {"character"}})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	other := idRe.FindStringSubmatch(rec.Body.String())[1]
	rec = a.form(http.MethodPost, base, url.Values{"title": {"舞台"}, "content": {"近未来の東京"}, "category": {"world"}})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var id string
	for _, m := range idRe.FindAllStringSubmatch(rec.Body.String(), -1) {
		if m[1] != other {
			id = m[1]
		}
	}
	require.NotEmpty(t, id)
	// Another tab adds a note the first tab's page does not show.
	rec = a.form(http.MethodPost, base, url.Values{"title": {"小道具"}, "content": {"赤い傘"}, "category": {"product"}})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var unseen string
	for _, m := range idRe.FindAllStringSubmatch(rec.Body.String(), -1) {
		if m[1] != other && m[1] != id {
			unseen = m[1]
		}
	}
	require.NotEmpty(t, unseen)

	// What the page reports: the hx-include of #note-list sends the on-page field of the notes it shows.
	onPage := url.Values{notelist.OnPageField: {id, other}}
	with := func(v url.Values) url.Values {
		for k, vs := range onPage {
			v[k] = vs
		}
		return v
	}
	answers := map[string]*httptest.ResponseRecorder{
		"archive":   a.htmx(http.MethodDelete, "/notes/"+id+"?"+onPage.Encode(), nil, ""),
		"unarchive": a.form(http.MethodPut, "/notes/"+id, with(url.Values{"archived": {"false"}})),
		"save":      a.form(http.MethodPut, "/notes/"+id, with(url.Values{"title": {"舞台"}, "content": {"近未来の大阪"}, "category": {"tone"}})),
		"restore":   a.form(http.MethodPost, "/notes/"+id+"/versions/1/restore", with(url.Values{})),
	}
	for name, rec := range answers {
		require.Equal(t, http.StatusOK, rec.Code, name+": "+rec.Body.String())
		body := rec.Body.String()
		assert.Contains(t, body, `id="note-list"`, name)
		assert.Contains(t, body, `id="note-content-`+id+`"`, name+": the note acted on is drawn in full")
		assert.NotContains(t, body, `id="note-content-`+other+`"`, name+": the form of another note is not replaced")
		assert.NotContains(t, body, "落ち着いた口調", name)
		assert.Contains(t, body, `<div id="note-item-`+other+`" hx-preserve></div>`, name+": the page keeps the other note")
		assert.Contains(t, body, `id="note-content-`+unseen+`"`, name+": a note the page does not have is drawn in full")
		assert.NotContains(t, body, `<div id="note-item-`+unseen+`" hx-preserve></div>`, name)
	}
	assert.Contains(t, answers["save"].Body.String(), `<section aria-label="トーン・口調"`, "a note that changed its category lands in its new group")
}

// Adding a note answers with the new note in full; the notes the page has are placeholders the page
// keeps, so the text typed into the form of another note survives the addition (#742). The add form
// reports the notes on the page (on-page) through the hx-include it carries.
func TestNotes_CreatingANoteDoesNotCarryTheFormsOfTheOthers(t *testing.T) {
	a := newApp(t)
	base := "/projects/" + a.createProject() + "/notes"
	idRe := regexp.MustCompile(`hx-put="/notes/([^"]+)"`)
	rec := a.form(http.MethodPost, base, url.Values{"title": {"口調"}, "content": {"落ち着いた口調"}, "category": {"character"}})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	existing := idRe.FindStringSubmatch(rec.Body.String())[1]

	rec = a.form(http.MethodPost, base, url.Values{
		"title": {"舞台"}, "content": {"近未来の東京"}, "category": {"world"},
		notelist.OnPageField: {existing},
	})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	body := rec.Body.String()
	var created string
	for _, m := range idRe.FindAllStringSubmatch(body, -1) {
		created = m[1]
	}
	require.NotEmpty(t, created)
	require.NotEqual(t, existing, created)
	assert.Contains(t, body, `id="note-content-`+created+`"`, "the new note is drawn in full")
	assert.NotContains(t, body, `id="note-content-`+existing+`"`, "the form of another note is not replaced")
	assert.NotContains(t, body, "落ち着いた口調")
	assert.Contains(t, body, `<div id="note-item-`+existing+`" hx-preserve></div>`, "the page keeps the other note")
	assert.Regexp(t, `(?s)<form\s+id="note-add-form"[^>]*hx-include="#note-list input\[name=&#39;`+notelist.OnPageField+`&#39;\]"`, body, "the add form sends on-page")
}

// Removing an image from a filtered list keeps the filter: the button carries it and the answer is the
// filtered list, with the search form showing it (#696).
func TestReferenceLibrary_DeleteKeepsTheFilterOfTheList(t *testing.T) {
	a := newApp(t)
	id := a.createProject()
	for _, in := range []struct{ kind, name string }{{"character", "Mio"}, {"character", "Rin"}, {"product", "Serum"}} {
		rec := a.multipart("/projects/"+id+"/references", map[string]string{"kind": in.kind, "name": in.name, "tags": "hero"}, in.name+".png")
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	}

	filtered := a.htmx(http.MethodGet, "/projects/"+id+"/references?kind=character&tag=hero", nil, "").Body.String()
	assert.Contains(t, filtered, `{&#34;kind&#34;:&#34;character&#34;,&#34;q&#34;:&#34;&#34;,&#34;tag&#34;:&#34;hero&#34;}`, "the remove button carries the filter")
	m := regexp.MustCompile(`hx-delete="/projects/[^/]+/references/([^"]+)"`).FindStringSubmatch(filtered)
	require.NotNil(t, m, filtered)

	del := a.htmx(http.MethodDelete, "/projects/"+id+"/references/"+m[1]+"?q=&kind=character&tag=hero", nil, "")
	require.Equal(t, http.StatusOK, del.Code, del.Body.String())
	body := del.Body.String()
	assert.NotContains(t, body, "Serum", "the filter still holds")
	assert.Equal(t, 1, strings.Count(body, `hx-delete="`), "one of the two characters is left")
	assert.Contains(t, body, `value="character" selected`, "the search form shows the kind")
	assert.Contains(t, body, `name="tag" type="text" value="hero"`, "and the tag")
	assert.Equal(t, http.StatusNotFound, a.htmx(http.MethodDelete, "/projects/"+id+"/references/nope?kind=bogus", nil, "").Code)
}

// Resolving a comment of a review answers with the review request that holds it, not the list: the
// note being written for the next request and the response file being chosen are in the list's forms
// and would be replaced (#730).
func TestReviews_ResolvingACommentDoesNotCarryTheFormsOfTheList(t *testing.T) {
	a := newApp(t)
	p := a.createProject()
	for _, q := range []string{
		`INSERT INTO export (id, project_id, file_path, resolution, aspect_ratio, codec) VALUES ('e1', '` + p + `', 'x.mp4', '1080p', '9:16', 'h264')`,
		`INSERT INTO review_request (id, project_id, export_id) VALUES ('rq1', '` + p + `', 'e1')`,
		`INSERT INTO review_response (id, review_request_id, reviewer_name, decision, created_at) VALUES ('rs1', 'rq1', 'Taro', 'changes_requested', '2026-10-16T00:00:00.000Z')`,
		`INSERT INTO review_comment (id, review_response_id, body) VALUES ('c1', 'rs1', 'ロゴが小さい')`,
	} {
		require.NoError(t, a.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, q)
			return err
		}))
	}

	page := a.htmx(http.MethodGet, "/projects/"+p+"/reviews", nil, "").Body.String()
	assert.Contains(t, page, `id="review-note"`)
	assert.Contains(t, page, `id="review-response-file"`)
	assert.Contains(t, page, `hx-target="closest article"`, "the button swaps its request only")

	rec := a.form(http.MethodPut, "/projects/"+p+"/review-comments/c1", url.Values{"resolved": {"true"}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.Contains(t, body, `id="review-rq1"`, "the request the comment belongs to")
	assert.Contains(t, body, "line-through", "the comment is resolved")
	assert.NotContains(t, body, "未解決のコメント", "the count of open comments is in the swapped part")
	assert.NotContains(t, body, `id="review-list"`)
	assert.NotContains(t, body, `id="review-note"`, "the note of the request form is not replaced")
	assert.NotContains(t, body, `id="review-response-file"`, "the chosen response file is not replaced")
	assert.Equal(t, http.StatusNotFound, a.form(http.MethodPut, "/projects/"+p+"/review-comments/nope", url.Values{"resolved": {"true"}}).Code)
}

// The reload of the stage regions (after the timeline editor saved) is what every stage answers like: it carries
// neither the export options nor the voice form, so what is typed or picked in them survives (#731). The page does carry them.
func TestEditor_StageAnswersDoNotCarryTheInputsOfTheForms(t *testing.T) {
	a := newApp(t)
	id := a.createProject()

	page := a.htmx(http.MethodGet, "/projects/"+id+"/edit", nil, "").Body.String()
	for _, input := range []string{`name="aspect_ratio"`, `name="sample"`, `id="editor-actions"`} {
		assert.Contains(t, page, input)
	}
	reload := a.htmx(http.MethodGet, "/projects/"+id+"/edit/actions", nil, "")
	require.Equal(t, http.StatusOK, reload.Code, reload.Body.String())
	for _, input := range []string{`name="aspect_ratio"`, `name="resolution"`, `name="codec"`, `name="speaker"`, `name="sample"`, `id="editor-actions"`} {
		assert.NotContains(t, reload.Body.String(), input)
	}
	assert.Contains(t, reload.Body.String(), `id="editor-export-submit"`)
}

// Selecting or excluding a Take answers with the whole Shot card, whose "追加の指示" input is new and
// empty: it is hx-preserve, so htmx keeps the one on the page with what is typed in it (#732).
func TestShotCard_TakeActionsKeepTheAdditionalInstruction(t *testing.T) {
	a := newApp(t)
	rec := a.form(http.MethodPost, "/projects/"+a.createProject()+"/scenes", url.Values{"title": {"Street"}})
	scene := regexp.MustCompile(`/scenes/([0-9a-f-]{36})`).FindStringSubmatch(rec.Body.String())[1]
	rec = a.form(http.MethodPost, "/scenes/"+scene+"/shots", url.Values{"objective": {"走り出す"}, "duration_seconds": {"4"}})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	shot := regexp.MustCompile(`id="shot-([0-9a-f-]{36})"`).FindStringSubmatch(rec.Body.String())[1]
	require.NoError(t, a.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		for _, q := range []string{
			`INSERT INTO prompt_version (id, shot_id, version_number, shot_spec_snapshot) VALUES ('pv1', '` + shot + `', 1, '{}')`,
			`INSERT INTO take (id, prompt_version_id, model_name, model_parameters, file_path) VALUES ('t1', 'pv1', 'kling-3.0', '{}', 'p/t1.mp4'), ('t2', 'pv1', 'kling-3.0', '{}', 'p/t2.mp4')`,
		} {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
		return nil
	}))

	input := regexp.MustCompile(`<input[^>]*id="instruction-` + shot + `"[^>]*>`)
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"select":  a.form(http.MethodPost, "/takes/t1/select", url.Values{"selected": {"true"}}),
		"exclude": a.form(http.MethodDelete, "/takes/t2?selected=true", nil),
	} {
		require.Equal(t, http.StatusOK, rec.Code, name+": "+rec.Body.String())
		assert.Contains(t, input.FindString(rec.Body.String()), "hx-preserve", name)
	}
}
