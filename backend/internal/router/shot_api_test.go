package router_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShotAPI_ReturnsTheShotAndUnknownIDIs404(t *testing.T) {
	a := newApp(t)
	project := a.createProject("美容液のCMを作りたい")
	rec := a.form(http.MethodPost, "/projects/"+project+"/scenes", url.Values{"title": {"Street"}})
	m := sceneIDRe.FindStringSubmatch(rec.Body.String())
	require.NotNil(t, m)
	rec = a.form(http.MethodPost, "/scenes/"+m[1]+"/shots", url.Values{"objective": {"走り出す"}, "duration_seconds": {"4"}})
	require.Equal(t, http.StatusCreated, rec.Code)
	s := shotIDRe.FindStringSubmatch(rec.Body.String())
	require.NotNil(t, s)

	rec = a.get("/api/v1/shots/"+s[1], false)
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
	assert.Equal(t, s[1], body.ID)
	assert.Equal(t, m[1], body.SceneID)
	assert.Equal(t, project, body.ProjectID)
	assert.Equal(t, "走り出す", body.Objective)
	assert.InDelta(t, 4, body.DurationSeconds, 0)
	assert.JSONEq(t, "{}", string(body.ShotSpec))
	assert.NotEmpty(t, body.State)
	assert.Empty(t, body.SelectedTakeID, "no Take is selected yet")

	assert.Equal(t, http.StatusNotFound, a.get("/api/v1/shots/nope", false).Code)
}
