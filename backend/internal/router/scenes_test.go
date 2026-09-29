package router_test

import (
	"net/http"
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
