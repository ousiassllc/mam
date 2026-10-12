package router_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var noteIDRe = regexp.MustCompile(`hx-put="/notes/([^"]+)"`)

func TestNotes_CreateUpdateVersionRestoreAndArchiveOnTheNotesPage(t *testing.T) {
	a := newApp(t)
	p := a.createProject("美容液の縦型15秒CMを作りたい")
	base := "/projects/" + p + "/notes"

	page := a.get(base, false)
	require.Equal(t, http.StatusOK, page.Code)
	assert.Contains(t, page.Body.String(), "<html")
	assert.Contains(t, page.Body.String(), "メモはまだありません")
	assert.Contains(t, a.get("/projects/"+p, false).Body.String(), `href="`+base+`"`, "the workspace links to the Notes")

	rec := a.form(http.MethodPost, base, url.Values{"title": {"口調"}, "content": {"落ち着いた口調"}, "category": {"tone"}, "pinned": {"false", "true"}})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "<html")
	assert.Contains(t, rec.Body.String(), "ピン留め")
	assert.Contains(t, rec.Body.String(), "落ち着いた口調")
	m := noteIDRe.FindStringSubmatch(rec.Body.String())
	require.NotNil(t, m, rec.Body.String())
	id := m[1]

	// A category change keeps the version; a new text appends one.
	rec = a.form(http.MethodPut, "/notes/"+id, url.Values{"category": {"world"}, "pinned": {"false", "false"}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "v1")
	assert.NotContains(t, rec.Body.String(), "v2")
	rec = a.form(http.MethodPut, "/notes/"+id, url.Values{"title": {"口調"}, "content": {"早口で話す"}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "v2")

	// version_history_panel restores v1 as a new version.
	assert.Contains(t, rec.Body.String(), `hx-post="/notes/`+id+`/versions/1/restore"`)
	rec = a.form(http.MethodPost, "/notes/"+id+"/versions/1/restore", url.Values{})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "v3")
	assert.Contains(t, rec.Body.String(), "落ち着いた口調")

	rec = a.form(http.MethodPost, "/notes/"+id+"/versions/9/restore", url.Values{})
	assert.Equal(t, http.StatusNotFound, rec.Code)
	rec = a.form(http.MethodPost, "/notes/"+id+"/versions/x/restore", url.Values{})
	assert.GreaterOrEqual(t, rec.Code, 400)

	rec = a.form(http.MethodDelete, "/notes/"+id, url.Values{})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "アーカイブ済み (1)")
	rec = a.form(http.MethodPut, "/notes/"+id, url.Values{"archived": {"false"}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "アーカイブ済み (")

	rec = a.form(http.MethodPost, base, url.Values{"title": {"x"}, "content": {"  "}})
	assert.GreaterOrEqual(t, rec.Code, 400)
	assert.Equal(t, http.StatusNotFound, a.get("/projects/nope/notes", false).Code)
}

// An action on one note answers with the note it is about; the other notes are placeholders that the page
// keeps as they are, so the text typed into their forms survives (#694).
func TestNotes_AnActionOnOneNoteDoesNotCarryTheFormsOfTheOthers(t *testing.T) {
	a := newApp(t)
	p := a.createProject("美容液の縦型15秒CMを作りたい")
	base := "/projects/" + p + "/notes"
	rec := a.form(http.MethodPost, base, url.Values{"title": {"口調"}, "content": {"落ち着いた口調"}, "category": {"character"}})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	other := noteIDRe.FindStringSubmatch(rec.Body.String())[1]
	rec = a.form(http.MethodPost, base, url.Values{"title": {"舞台"}, "content": {"近未来の東京"}, "category": {"world"}})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var id string
	for _, m := range noteIDRe.FindAllStringSubmatch(rec.Body.String(), -1) {
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
		assert.Contains(t, body, `id="note-content-`+id+`"`, name+": the note acted on is drawn in full")
		assert.NotContains(t, body, `id="note-content-`+other+`"`, name+": the form of another note is not replaced")
		assert.NotContains(t, body, "落ち着いた口調", name)
		assert.Contains(t, body, `<div id="note-item-`+other+`" hx-preserve></div>`, name+": the page keeps the other note")
		assert.Contains(t, body, `id="note-list"`, name)
	}
	assert.Contains(t, answers["save"].Body.String(), `<section aria-label="トーン・口調"`, "a note that changed its category lands in the group of the new one")
}

func TestNotes_SeriesNotesPage(t *testing.T) {
	a := newApp(t)
	src := a.createProject("美容液の縦型15秒CMを作りたい")
	rec := a.form(http.MethodPost, "/series", url.Values{"name": {"S"}, "from_project_id": {src}})
	require.Equal(t, http.StatusCreated, rec.Code)
	seriesID := strings.TrimPrefix(rec.Header().Get("HX-Redirect"), "/series/")

	assert.Contains(t, a.get("/series/"+seriesID, false).Body.String(), `href="/series/`+seriesID+`/notes"`)
	rec = a.form(http.MethodPost, "/series/"+seriesID+"/notes", url.Values{"title": {"世界観"}, "content": {"近未来の東京"}, "category": {"world"}})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	page := a.get("/series/"+seriesID+"/notes", false)
	require.Equal(t, http.StatusOK, page.Code)
	assert.Contains(t, page.Body.String(), "近未来の東京")

	rec = a.form(http.MethodPost, "/series/"+seriesID+"/episodes", url.Values{})
	require.Equal(t, http.StatusCreated, rec.Code)
	ep := strings.TrimPrefix(rec.Header().Get("HX-Redirect"), "/projects/")
	assert.Contains(t, a.get("/projects/"+ep+"/notes", false).Body.String(), "近未来の東京", "the Episode got a copy")
}
