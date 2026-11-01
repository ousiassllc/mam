package router_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The server holds to the actions it offers (select_url / reject_url): an excluded Take cannot be
// brought back by a direct POST, and a Take that is already selected or excluded is refused again, so
// none of them adds a human intervention.
func TestTakes_RefuseWhatTheyDoNotOfferAndRecordNoIntervention(t *testing.T) {
	ta := newTakeApp(t)
	interventions := func() int {
		var n int
		require.NoError(t, ta.db.Reader().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM human_intervention`).Scan(&n))
		return n
	}
	require.Equal(t, http.StatusOK, ta.form(http.MethodPost, "/takes/t1/select", nil).Code)
	require.Equal(t, http.StatusOK, ta.form(http.MethodDelete, "/takes/t2", nil).Code)
	before := interventions()

	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/takes/t2/select"}, // excluded
		{http.MethodPost, "/takes/t1/select"}, // already selected
		{http.MethodDelete, "/takes/t2"},      // already excluded
	} {
		rec := ta.form(c.method, c.path, nil)
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, c.method+" "+c.path+": "+rec.Body.String())
	}

	assert.Equal(t, before, interventions())
	assert.Equal(t, "rejected", ta.statusOf("t2"), "the excluded Take stays excluded")
	assert.Equal(t, "selected", ta.statusOf("t1"))
}

func (ta *takeApp) statusOf(id string) string {
	ta.t.Helper()
	for _, tk := range ta.takes().Takes {
		if tk.ID == id {
			return tk.Status
		}
	}
	ta.t.Fatalf("no take %s", id)
	return ""
}

// The comparison names a Take's status as the Shot card does (domain.TakeStatus.Label), so the
// client holds no label of its own.
func TestTakeAPI_NamesEachStatusAsTheShotCardDoes(t *testing.T) {
	ta := newTakeApp(t)
	list := func() string { return ta.get("/api/v1/shots/"+ta.shotID+"/takes", false).Body.String() }
	assert.Contains(t, list(), `"status_label":"候補"`)

	require.Equal(t, http.StatusOK, ta.form(http.MethodPost, "/takes/t1/select", nil).Code)
	require.Equal(t, http.StatusOK, ta.form(http.MethodDelete, "/takes/t2", nil).Code)
	assert.Contains(t, list(), `"status_label":"採用"`)
	assert.Contains(t, list(), `"status_label":"除外"`)
}

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
