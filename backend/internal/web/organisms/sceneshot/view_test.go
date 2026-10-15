package sceneshot_test

import (
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/molecules"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/sceneshot"
)

func renderHTML(t *testing.T, c templ.Component) string {
	t.Helper()
	var sb strings.Builder
	require.NoError(t, c.Render(context.Background(), &sb))
	return sb.String()
}

func data() sceneshot.Data {
	one := domain.Scene{ID: "sc1", Title: "導入"}
	two := domain.Scene{ID: "sc2", Title: "展開", OrderIndex: 1}
	return sceneshot.Data{
		Project: domain.Project{ID: "p1"},
		Scenes:  []sceneshot.Item{{Scene: one, ShotCount: 2}, {Scene: two}},
		Scene:   &one,
		Shots: []molecules.ShotCardData{
			{ProjectID: "p1", Shot: domain.Shot{ID: "sh1", SceneID: "sc1", Objective: "最初"}},
			{ProjectID: "p1", Shot: domain.Shot{ID: "sh2", SceneID: "sc1", Objective: "追加した", OrderIndex: 1}, Selected: true},
		},
	}
}

// The forms and the lists are named parts of the view, so a response can replace one of them without the rest (#733).
func TestView_NamesTheFormsAndTheLists(t *testing.T) {
	html := renderHTML(t, sceneshot.View(data()))

	for _, want := range []string{`id="scene-shot-view"`, `id="scene-list"`, `id="shot-list"`, `id="scene-form"`, `id="shot-form"`, `id="scene-shot-count-sc1"`} {
		assert.Contains(t, html, want)
	}
	assert.Equal(t, 2, strings.Count(html, `hx-target="this"`), "each form answers itself")
	assert.Contains(t, html, `name="current_scene_id" value="sc1"`)
}

func TestSceneRejected_AnswersTheSceneFormWithItsInputOnly(t *testing.T) {
	d := data()
	d.SceneErrors = map[string]string{"title": "見出しを入力してください"}
	d.SceneInput = sceneshot.SceneInput{Location: "残る場所"}
	html := renderHTML(t, sceneshot.SceneRejected(d, "sc1"))

	assert.Contains(t, html, `id="scene-form"`)
	assert.Contains(t, html, "見出しを入力してください")
	assert.Contains(t, html, `value="残る場所"`)
	assert.Contains(t, html, `name="current_scene_id" value="sc1"`)
	for _, other := range []string{`id="shot-form"`, `name="duration_seconds"`, `id="scene-shot-view"`, `id="shot-list"`} {
		assert.NotContains(t, html, other)
	}
}

func TestSceneAdded_AppendsTheLastSceneAndKeepsTheShownOne(t *testing.T) {
	html := renderHTML(t, sceneshot.SceneAdded(data(), "sc1"))

	assert.Contains(t, html, `id="scene-form"`)
	assert.Contains(t, html, `name="current_scene_id" value="sc1"`)
	assert.Contains(t, html, `hx-swap-oob="beforeend:#scene-list"`)
	assert.Contains(t, html, "Scene 2: 展開")
	assert.NotContains(t, html, "Scene 1: 導入", "only the new Scene is appended")
	assert.NotContains(t, html, `aria-current="true"`, "the new Scene is not the one shown")
	for _, other := range []string{`id="shot-form"`, `name="duration_seconds"`, `id="scene-shot-view"`, `id="shot-list"`} {
		assert.NotContains(t, html, other)
	}
}

func TestShotRejected_AnswersTheShotFormWithItsInputOnly(t *testing.T) {
	d := data()
	d.ShotErrors = map[string]string{"duration_seconds": "秒数が不正です"}
	d.ShotInput = sceneshot.ShotInput{Objective: "残る目的", DurationSeconds: "abc"}
	html := renderHTML(t, sceneshot.ShotRejected(d))

	assert.Contains(t, html, `id="shot-form"`)
	assert.Contains(t, html, "秒数が不正です")
	assert.Contains(t, html, `value="残る目的"`)
	for _, other := range []string{`id="scene-form"`, `name="location"`, `id="scene-shot-view"`, `id="shot-list"`} {
		assert.NotContains(t, html, other)
	}
}

// The new Shot is appended unselected and alone: the cards already listed, with the instruction typed into the selected
// one, are not replaced.
func TestShotAdded_AppendsOnlyTheNewShotAndUpdatesTheCount(t *testing.T) {
	d := data()
	d.Scenes[0].ShotCount = 2
	html := renderHTML(t, sceneshot.ShotAdded(d))

	assert.Contains(t, html, `id="shot-form"`)
	assert.Contains(t, html, `hx-swap-oob="beforeend:#shot-list"`)
	assert.Contains(t, html, `id="shot-sh2"`)
	assert.NotContains(t, html, `id="shot-sh1"`)
	assert.NotContains(t, html, `aria-current="true"`)
	assert.NotContains(t, html, `id="shot-empty"`)
	assert.Contains(t, html, `<span id="scene-shot-count-sc1" class="block text-xs text-neutral-400" hx-swap-oob="true">2 Shot</span>`)
	for _, other := range []string{`id="scene-form"`, `name="location"`, `id="scene-shot-view"`, `id="scene-list"`} {
		assert.NotContains(t, html, other)
	}

	d.Shots = d.Shots[1:]
	assert.Contains(t, renderHTML(t, sceneshot.ShotAdded(d)), `<li id="shot-empty" hx-swap-oob="delete"></li>`, "the first Shot removes the empty message")
}
