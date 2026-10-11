package router_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var sceneIDRe = regexp.MustCompile(`/projects/[^/"]+/scenes/([0-9a-f-]{36})`)

func TestScenes_EmptyProjectShowsTheFormsAndUnknownIDsAre404(t *testing.T) {
	a := newApp(t)
	id := a.createProject("美容液の縦型15秒CMを作りたい")

	rec := a.get("/projects/"+id+"/scenes", false)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "<html")
	assert.Contains(t, rec.Body.String(), `id="scene-shot-view"`)
	assert.Contains(t, rec.Body.String(), `hx-post="/projects/`+id+`/scenes"`)

	assert.Equal(t, http.StatusNotFound, a.get("/projects/"+id+"/scenes/nope", false).Code)
	assert.Equal(t, http.StatusNotFound, a.get("/projects/nope/scenes", false).Code)
	assert.Equal(t, http.StatusNotFound, a.form(http.MethodPost, "/scenes/nope/shots", url.Values{"objective": {"x"}, "duration_seconds": {"3"}}).Code)
}

// main.ts swaps the toast of a 404/5xx, and htmx then pushes the URL of an hx-push-url link even with
// HX-Reswap: none: the server must tell it not to (a toast answer never changes the address bar).
func TestScenes_FailedHTMXNavigationAndActionDoNotPushTheURL(t *testing.T) {
	a := newApp(t)
	id := a.createProject("美容液の縦型15秒CMを作りたい")

	page := a.get("/projects/"+id+"/scenes/nope", true)
	require.Equal(t, http.StatusNotFound, page.Code)
	assert.Equal(t, "none", page.Header().Get("HX-Reswap"))
	assert.Equal(t, "false", page.Header().Get("HX-Push-Url"))

	action := a.form(http.MethodPost, "/scenes/nope/shots", url.Values{"objective": {"x"}, "duration_seconds": {"3"}})
	require.Equal(t, http.StatusNotFound, action.Code)
	assert.Equal(t, "none", action.Header().Get("HX-Reswap"))
	assert.Equal(t, "false", action.Header().Get("HX-Push-Url"))

	ok := a.get("/projects/"+id+"/scenes", true)
	require.Equal(t, http.StatusOK, ok.Code)
	assert.Empty(t, ok.Header().Get("HX-Push-Url"), "a page that loaded keeps hx-push-url's behaviour")
}

func TestScenes_CreateSceneAndShotThenSelectThem(t *testing.T) {
	a := newApp(t)
	id := a.createProject("美容液の縦型15秒CMを作りたい")

	rec := a.form(http.MethodPost, "/projects/"+id+"/scenes", url.Values{"title": {"導入"}, "location": {"スタジオ"}})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "<html", "the fragment only")
	assert.Contains(t, rec.Body.String(), "導入")
	m := sceneIDRe.FindStringSubmatch(rec.Body.String())
	require.NotNil(t, m, rec.Body.String())
	sceneID := m[1]

	rec = a.form(http.MethodPost, "/scenes/"+sceneID+"/shots", url.Values{"objective": {"商品を見せる"}, "duration_seconds": {"3.5"}})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "商品を見せる")
	assert.Contains(t, rec.Body.String(), "3.5 秒")

	page := a.get("/projects/"+id+"/scenes/"+sceneID, false)
	require.Equal(t, http.StatusOK, page.Code)
	assert.Contains(t, page.Body.String(), "<html")
	assert.Contains(t, page.Body.String(), "商品を見せる")
	frag := a.get("/projects/"+id+"/scenes/"+sceneID, true)
	assert.NotContains(t, frag.Body.String(), "<html")

	assert.Contains(t, a.get("/projects/"+id, false).Body.String(), `/projects/`+id+`/scenes"`, "the workspace links to the hierarchy")
}

func TestScenes_InvalidInputRerendersWithTheErrorAndTheUsersValues(t *testing.T) {
	a := newApp(t)
	id := a.createProject("美容液の縦型15秒CMを作りたい")

	rec := a.form(http.MethodPost, "/projects/"+id+"/scenes", url.Values{"title": {"  "}, "location": {"残る場所"}})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "残る場所", "the rejected input is kept")

	rec = a.form(http.MethodPost, "/projects/"+id+"/scenes", url.Values{"title": {"S"}})
	m := sceneIDRe.FindStringSubmatch(rec.Body.String())
	require.NotNil(t, m)
	rec = a.form(http.MethodPost, "/scenes/"+m[1]+"/shots", url.Values{"objective": {"残る目的"}, "duration_seconds": {"abc"}})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "残る目的")
	assert.Contains(t, rec.Body.String(), `value="abc"`)
}

func TestScenes_HistoryRestoreGetsTheFullPageNotTheFragment(t *testing.T) {
	a := newApp(t)
	id := a.createProject("美容液の縦型15秒CMを作りたい")
	rec := a.form(http.MethodPost, "/projects/"+id+"/scenes", url.Values{"title": {"導入"}})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	sceneID := sceneIDRe.FindStringSubmatch(rec.Body.String())[1]
	path := "/projects/" + id + "/scenes/" + sceneID

	get := func(headers map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(a.cookie)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		return do(a.engine, req)
	}

	restore := get(map[string]string{"HX-Request": "true", "HX-History-Restore-Request": "true"})
	require.Equal(t, http.StatusOK, restore.Code)
	assert.Contains(t, restore.Body.String(), "<!doctype html>", "htmx swaps the whole body on a history restore")
	assert.Contains(t, restore.Body.String(), `name="csrf-token"`)
	assert.Contains(t, restore.Header().Get("Vary"), "HX-History-Restore-Request")

	fragment := get(map[string]string{"HX-Request": "true"})
	require.Equal(t, http.StatusOK, fragment.Code)
	assert.NotContains(t, fragment.Body.String(), "<html")
	assert.NotContains(t, fragment.Body.String(), `name="csrf-token"`)
}
