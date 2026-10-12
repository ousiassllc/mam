// Package swap_test checks which part of the screen an HTMX answer replaces: an action on one row must
// not carry the forms of the other rows, nor lose the filter of the list it was taken from.
package swap_test

import (
	"bytes"
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
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/testenv"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
)

func init() { gin.SetMode(gin.TestMode) }

type app struct {
	t      *testing.T
	engine *gin.Engine
	cookie *http.Cookie
	token  string
}

// newApp is an app with its own database that does what a browser does: it keeps the CSRF cookie and
// sends the token with every request.
func newApp(t *testing.T) *app {
	t.Helper()
	testenv.Parallel(t)
	svc := servicetest.New(t, service.Deps{DB: testdb.Open(t), Credentials: credentialtest.New(), AssetsDir: t.TempDir()})
	a := &app{t: t, engine: router.New(svc, router.Options{})}
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

// An action on one note answers with that note in full; the other notes are placeholders the page keeps,
// so the text typed into their forms survives (#694).
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

	answers := map[string]*httptest.ResponseRecorder{
		"archive":   a.form(http.MethodDelete, "/notes/"+id, url.Values{}),
		"unarchive": a.form(http.MethodPut, "/notes/"+id, url.Values{"archived": {"false"}}),
		"save":      a.form(http.MethodPut, "/notes/"+id, url.Values{"title": {"舞台"}, "content": {"近未来の大阪"}, "category": {"tone"}}),
		"restore":   a.form(http.MethodPost, "/notes/"+id+"/versions/1/restore", url.Values{}),
	}
	for name, rec := range answers {
		require.Equal(t, http.StatusOK, rec.Code, name+": "+rec.Body.String())
		body := rec.Body.String()
		assert.Contains(t, body, `id="note-list"`, name)
		assert.Contains(t, body, `id="note-content-`+id+`"`, name+": the note acted on is drawn in full")
		assert.NotContains(t, body, `id="note-content-`+other+`"`, name+": the form of another note is not replaced")
		assert.NotContains(t, body, "落ち着いた口調", name)
		assert.Contains(t, body, `<div id="note-item-`+other+`" hx-preserve></div>`, name+": the page keeps the other note")
	}
	assert.Contains(t, answers["save"].Body.String(), `<section aria-label="トーン・口調"`, "a note that changed its category lands in its new group")
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
