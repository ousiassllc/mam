package swap_test

import (
	"net/http"
	"net/url"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Adding a Scene or a Shot answers with the form that sent it and the part it changes, never the whole view: the other
// form (and the selected Shot card) would be replaced by an empty one and lose what is typed into it (#733).
func TestScenes_AddingASceneOrAShotDoesNotCarryTheOtherForm(t *testing.T) {
	a := newApp(t)
	p := a.createProject()
	idRe := regexp.MustCompile(`/projects/` + p + `/scenes/([0-9a-f-]{36})`)

	// The first Scene: the page shows none and has no Shot form, so its Scene form takes the whole view.
	page := a.htmx(http.MethodGet, "/projects/"+p+"/scenes", nil, "")
	assert.Contains(t, page.Body.String(), `hx-target="#scene-shot-view"`)
	rec := a.form(http.MethodPost, "/projects/"+p+"/scenes", url.Values{"title": {"導入"}})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `id="scene-shot-view"`)
	scene := idRe.FindStringSubmatch(rec.Body.String())[1]

	// A second Scene while the first is shown: the Scene form and the new list item; no Shot form.
	rec = a.form(http.MethodPost, "/projects/"+p+"/scenes", url.Values{"title": {"展開"}, "current_scene_id": {scene}})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.Contains(t, body, `id="scene-form"`)
	assert.Contains(t, body, `hx-swap-oob="beforeend:#scene-list"`)
	assert.Contains(t, body, "Scene 2: 展開")
	assert.Contains(t, body, `name="current_scene_id" value="`+scene+`"`, "the Scene shown is still the one shown")
	for _, other := range []string{`id="scene-shot-view"`, `id="shot-form"`, `name="duration_seconds"`, `id="shot-list"`} {
		assert.NotContains(t, body, other)
	}

	// A rejected Scene answers the Scene form only.
	rec = a.form(http.MethodPost, "/projects/"+p+"/scenes", url.Values{"title": {" "}, "location": {"残る場所"}, "current_scene_id": {scene}})
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `id="scene-form"`)
	assert.Contains(t, rec.Body.String(), `value="残る場所"`)
	assert.NotContains(t, rec.Body.String(), `name="duration_seconds"`)

	// A Shot: the Shot form, the new card appended unselected, the empty message removed and the count of its Scene.
	rec = a.form(http.MethodPost, "/scenes/"+scene+"/shots", url.Values{"objective": {"商品を見せる"}, "duration_seconds": {"3"}})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	body = rec.Body.String()
	assert.Contains(t, body, `id="shot-form"`)
	assert.Contains(t, body, `hx-swap-oob="beforeend:#shot-list"`)
	assert.Contains(t, body, "商品を見せる")
	assert.NotContains(t, body, `aria-current="true"`, "the new Shot does not take the selection from the one selected")
	assert.Contains(t, body, `id="shot-empty" hx-swap-oob="delete"`)
	assert.Contains(t, body, `id="scene-shot-count-`+scene+`"`)
	assert.Contains(t, body, "1 Shot")
	for _, other := range []string{`id="scene-shot-view"`, `id="scene-form"`, `name="location"`, `id="scene-list"`} {
		assert.NotContains(t, body, other)
	}
	rec = a.form(http.MethodPost, "/scenes/"+scene+"/shots", url.Values{"objective": {"二つ目"}, "duration_seconds": {"2"}})
	assert.NotContains(t, rec.Body.String(), `id="shot-empty"`, "only the first Shot removes the empty message")
	assert.Contains(t, rec.Body.String(), "2 Shot")

	// A rejected Shot answers the Shot form only.
	rec = a.form(http.MethodPost, "/scenes/"+scene+"/shots", url.Values{"objective": {"残る目的"}, "duration_seconds": {"abc"}})
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `id="shot-form"`)
	assert.Contains(t, rec.Body.String(), `value="残る目的"`)
	for _, other := range []string{`name="location"`, `id="scene-form"`, `id="shot-list"`} {
		assert.NotContains(t, rec.Body.String(), other)
	}
}
