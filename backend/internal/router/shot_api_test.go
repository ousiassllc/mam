package router_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createShot adds a Scene and a 4 s Shot to the Project and returns their IDs.
func (a *app) createShot(project string) (sceneID, shotID string) {
	a.t.Helper()
	rec := a.form(http.MethodPost, "/projects/"+project+"/scenes", url.Values{"title": {"Street"}})
	m := sceneIDRe.FindStringSubmatch(rec.Body.String())
	require.NotNil(a.t, m)
	sceneID = m[1]
	rec = a.form(http.MethodPost, "/scenes/"+sceneID+"/shots", url.Values{"objective": {"走り出す"}, "duration_seconds": {"4"}})
	require.Equal(a.t, http.StatusCreated, rec.Code, rec.Body.String())
	m = shotIDRe.FindStringSubmatch(rec.Body.String())
	require.NotNil(a.t, m, rec.Body.String())
	return sceneID, m[1]
}

func TestShotAPI_ReturnsTheShotAndUnknownIDIs404(t *testing.T) {
	a := newApp(t)
	project := a.createProject("美容液のCMを作りたい")
	scene, shot := a.createShot(project)

	rec := a.get("/api/v1/shots/"+shot, false)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body struct {
		ID              string          `json:"id"`
		SceneID         string          `json:"scene_id"`
		ProjectID       string          `json:"project_id"`
		Objective       string          `json:"objective"`
		DurationSeconds float64         `json:"duration_seconds"`
		ShotSpec        json.RawMessage `json:"shot_spec"`
		State           string          `json:"state"`
		SelectedTakeID  string          `json:"selected_take_id"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, shot, body.ID)
	assert.Equal(t, scene, body.SceneID)
	assert.Equal(t, project, body.ProjectID)
	assert.Equal(t, "走り出す", body.Objective)
	assert.InDelta(t, 4, body.DurationSeconds, 0)
	assert.JSONEq(t, "{}", string(body.ShotSpec))
	assert.NotEmpty(t, body.State)
	assert.Empty(t, body.SelectedTakeID, "no Take is selected yet")

	assert.Equal(t, http.StatusNotFound, a.get("/api/v1/shots/nope", false).Code)
}
