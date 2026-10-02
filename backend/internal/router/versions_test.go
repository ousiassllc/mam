package router_test

import (
	"net/http"
	"net/url"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const specA = `{"shot_objective":"A woman walks","camera":{"framing":"wide","movement":"static"},"acting":[{"time":"0.0-4.0","action":"walks"}]}`
const specB = `{"shot_objective":"A woman runs","camera":{"framing":"wide","movement":"static"},"acting":[{"time":"0.0-4.0","action":"runs"}]}`

func (g *genApp) editSpec(spec string) {
	g.t.Helper()
	require.Equal(g.t, http.StatusOK, g.form(http.MethodPut, "/shots/"+g.shotID+"/shot-spec", url.Values{"shot_spec": {spec}}).Code)
}

func TestShotVersions_InspectorListsThemAndRestoreAppendsACopy(t *testing.T) {
	g := newGenApp(t, false)
	g.editSpec(specA)
	g.editSpec(specB)

	page := g.get("/shots/"+g.shotID, false).Body.String()
	assert.Contains(t, page, `id="version-history-shot-`+g.shotID+`"`, "the inspector carries the version_history_panel")
	assert.Contains(t, page, `hx-post="/shots/`+g.shotID+`/versions/1/restore"`, "an older version has a restore button")
	assert.NotContains(t, page, `/versions/2/restore"`, "the newest version is the current one")
	assert.Contains(t, page, "現在")

	rec := g.form(http.MethodPost, "/shots/"+g.shotID+"/versions/1/restore", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.Contains(t, body, `id="shot-inspector"`, "the refreshed inspector is the response")
	assert.NotContains(t, body, "<html")
	assert.Contains(t, body, "A woman walks")
	assert.Contains(t, body, `hx-post="/shots/`+g.shotID+`/versions/2/restore"`, "the run that was current is now a restorable version")
	assert.NotContains(t, body, `/versions/3/restore"`)

	// Restoring is itself undoable.
	rec = g.form(http.MethodPost, "/shots/"+g.shotID+"/versions/2/restore", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "A woman runs")
}

func TestShotVersions_RestoreRefusals(t *testing.T) {
	g := newGenApp(t, false)
	g.editSpec(specA)
	g.editSpec(specB)
	restore := func(shot, n string) int {
		return g.form(http.MethodPost, "/shots/"+shot+"/versions/"+n+"/restore", nil).Code
	}

	assert.Equal(t, http.StatusNotFound, restore(g.shotID, "99"), "unknown version")
	assert.Equal(t, http.StatusNotFound, restore("nope", "1"), "unknown shot")
	assert.Equal(t, http.StatusUnprocessableEntity, restore(g.shotID, "abc"))
	assert.Equal(t, http.StatusUnprocessableEntity, restore(g.shotID, "0"))

	require.Equal(t, http.StatusOK, g.form(http.MethodPut, "/shots/"+g.shotID+"/locks", url.Values{"parameter": {"prompt"}, "mode": {"lock"}}).Code)
	rec := g.form(http.MethodPost, "/shots/"+g.shotID+"/versions/1/restore", nil)
	assert.Equal(t, http.StatusLocked, rec.Code, "Prompt is LOCKed: the spec must not change")
	assert.Equal(t, "none", rec.Header().Get("HX-Reswap"))
	assert.Contains(t, g.get("/shots/"+g.shotID, true).Body.String(), "A woman runs")
}

func TestWorldStateVersions_ViewListsThemAndRestoreBringsTheValuesBack(t *testing.T) {
	a := newApp(t)
	id := a.createProject("美容液の縦型15秒CMを作りたい")
	a.execSQL(`INSERT INTO character (id, project_id, name) VALUES ('c1', ?, 'Mio')`, id)
	require.Equal(t, http.StatusOK, a.form(http.MethodPut, "/projects/"+id+"/characters/c1", url.Values{"wardrobe": {"red_dress"}}).Code)
	require.Equal(t, http.StatusOK, a.form(http.MethodPut, "/projects/"+id+"/characters/c1", url.Values{"wardrobe": {"blue_coat"}}).Code)

	page := a.get("/projects/"+id+"/world-state", false).Body.String()
	assert.Contains(t, page, `id="version-history-world"`)
	assert.Contains(t, page, `hx-post="/projects/`+id+`/world-state/versions/1/restore"`)
	assert.NotContains(t, page, `/versions/2/restore"`)

	rec := a.form(http.MethodPost, "/projects/"+id+"/world-state/versions/1/restore", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `value="red_dress"`)
	assert.NotContains(t, rec.Body.String(), "blue_coat")
	assert.Contains(t, rec.Body.String(), `/versions/2/restore"`, "the restore was appended as version 3")

	assert.Equal(t, http.StatusNotFound, a.form(http.MethodPost, "/projects/"+id+"/world-state/versions/9/restore", nil).Code)
	assert.Equal(t, http.StatusNotFound, a.form(http.MethodPost, "/projects/nope/world-state/versions/1/restore", nil).Code)
	assert.Equal(t, http.StatusUnprocessableEntity, a.form(http.MethodPost, "/projects/"+id+"/world-state/versions/x/restore", nil).Code)

	a.execSQL(`INSERT INTO lock (id, project_id, target_type, target_id, parameter_name, mode, value) VALUES ('k1', ?, 'character', 'c1', 'wardrobe', 'lock', 'null')`, id)
	rec = a.form(http.MethodPost, "/projects/"+id+"/world-state/versions/2/restore", nil)
	assert.Equal(t, http.StatusLocked, rec.Code, "restoring would change a LOCKed field")
}

func TestWorldStateVersions_BranchPageRestoresTheBranchSeriesNotTheTrunk(t *testing.T) {
	a := newApp(t)
	id := a.createProject("美容液の縦型15秒CMを作りたい")
	a.execSQL(`INSERT INTO scene (id, project_id, order_index) VALUES ('sc', ?, 0)`, id)
	a.execSQL(`INSERT INTO shot (id, scene_id, order_index) VALUES ('sh', 'sc', 0)`)
	a.execSQL(`INSERT INTO branch (id, project_id, branch_point_type, branch_point_shot_id, name) VALUES ('b1', ?, 'shot', 'sh', 'alt')`, id)
	a.execSQL(`INSERT INTO character (id, project_id, name) VALUES ('c1', ?, 'Mio')`, id)
	a.execSQL(`INSERT INTO character (id, project_id, branch_id, name) VALUES ('c1b', ?, 'b1', 'Mio')`, id)
	require.Equal(t, http.StatusOK, a.form(http.MethodPut, "/projects/"+id+"/characters/c1", url.Values{"wardrobe": {"trunk_coat"}}).Code)
	require.Equal(t, http.StatusOK, a.form(http.MethodPut, "/projects/"+id+"/characters/c1b?branch_id=b1", url.Values{"wardrobe": {"red_dress"}}).Code)
	require.Equal(t, http.StatusOK, a.form(http.MethodPut, "/projects/"+id+"/characters/c1b?branch_id=b1", url.Values{"wardrobe": {"blue_coat"}}).Code)

	page := a.get("/projects/"+id+"/world-state?branch_id=b1", false).Body.String()
	assert.Contains(t, page, `hx-post="/projects/`+id+`/world-state/versions/1/restore?branch_id=b1"`, "the restore button is scoped to the Branch")
	assert.NotContains(t, page, `/versions/1/restore"`)

	rec := a.form(http.MethodPost, "/projects/"+id+"/world-state/versions/1/restore?branch_id=b1", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `value="red_dress"`, "the Branch view is the response")
	assert.NotContains(t, rec.Body.String(), "trunk_coat")
	assert.Contains(t, rec.Body.String(), `/versions/2/restore?branch_id=b1"`, "the response keeps the Branch scope and the restore was appended as a Branch version")

	var trunk int
	require.NoError(t, a.db.Reader().QueryRow(`SELECT COUNT(*) FROM world_state_version WHERE project_id = ? AND branch_id IS NULL`, id).Scan(&trunk))
	assert.Equal(t, 1, trunk, "the trunk series is untouched (one version from its own edit)")
	assert.Equal(t, http.StatusNotFound, a.form(http.MethodPost, "/projects/"+id+"/world-state/versions/1/restore?branch_id=nope", nil).Code)
}

func TestBranchVersions_HistoryAndRestores(t *testing.T) {
	a := newApp(t)
	id := a.createProject("美容液の縦型15秒CMを作りたい")
	a.execSQL(`INSERT INTO scene (id, project_id, order_index) VALUES ('sc', ?, 0)`, id)
	a.execSQL(`INSERT INTO shot (id, scene_id, order_index) VALUES ('sh', 'sc', 0)`)
	a.execSQL(`INSERT INTO branch (id, project_id, branch_point_type, branch_point_shot_id, name) VALUES ('b1', ?, 'shot', 'sh', 'renamed')`, id)
	a.execSQL(`INSERT INTO branch_version (id, branch_id, version_number, name) VALUES ('v1', 'b1', 1, 'original'), ('v2', 'b1', 2, 'renamed')`)
	a.execSQL(`INSERT INTO world_state_version (id, project_id, branch_id, version_number, state) VALUES ('w1', ?, 'b1', 1, '{"characters":[{"id":"x","name":"A"}]}'), ('w2', ?, 'b1', 2, '{}')`, id, id)

	detail := a.get("/branches/b1", true).Body.String()
	assert.Contains(t, detail, `hx-get="/branches/b1/versions"`, "the Branch page reaches the history and its restore buttons")
	assert.Contains(t, detail, `hx-trigger="branch-reload from:body"`, "the detail reloads after a restore")

	page := a.get("/branches/b1/versions", true).Body.String()
	assert.Contains(t, page, `id="branch-version-history"`)
	assert.Contains(t, page, `hx-post="/branches/b1/versions/1/restore"`)
	assert.Contains(t, page, `hx-post="/branches/b1/world-state/versions/1/restore"`)
	assert.Contains(t, page, "original")
	assert.Equal(t, http.StatusNotFound, a.get("/branches/nope/versions", true).Code)

	rec := a.form(http.MethodPost, "/branches/b1/versions/1/restore", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `/branches/b1/versions/2/restore"`, "history refreshed with the new version 3")
	assert.Equal(t, "branch-reload", rec.Header().Get("HX-Trigger"), "the Branch detail shown on the page reloads")
	var name string
	require.NoError(t, a.db.Reader().QueryRow(`SELECT name FROM branch WHERE id = 'b1'`).Scan(&name))
	assert.Equal(t, "original", name)

	rec = a.form(http.MethodPost, "/branches/b1/world-state/versions/1/restore", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var n int
	require.NoError(t, a.db.Reader().QueryRow(`SELECT COUNT(*) FROM world_state_version WHERE branch_id = 'b1'`).Scan(&n))
	assert.Equal(t, 3, n)

	for _, p := range []string{"/branches/b1/versions/9/restore", "/branches/nope/versions/1/restore", "/branches/b1/world-state/versions/9/restore", "/branches/nope/world-state/versions/1/restore"} {
		assert.Equal(t, http.StatusNotFound, a.form(http.MethodPost, p, nil).Code, p)
	}
	assert.Equal(t, http.StatusUnprocessableEntity, a.form(http.MethodPost, "/branches/b1/versions/x/restore", nil).Code)
}

var takeIDRe = regexp.MustCompile(`/takes/([0-9a-f-]{36})/trace`)

func TestTakeTrace_ShowsWhatLedToTheTake(t *testing.T) {
	g := newGenApp(t, true)
	g.execSQL(`INSERT INTO world_state_version (id, project_id, version_number, state) VALUES ('w1', ?, 1, '{}')`, g.project)
	g.execSQL(`UPDATE shot SET shot_spec = ? WHERE id = ?`,
		`{"references":[{"kind":"character","name":"Mio","asset_id":"missing-asset","prompt":"portrait","model":"nano-banana-2"}]}`, g.shotID)
	g.generateAndWait()

	inspector := g.get("/shots/"+g.shotID, true).Body.String()
	m := takeIDRe.FindStringSubmatch(inspector)
	require.NotNil(t, m, "every Take of the inspector offers its trace")
	takeID := m[1]

	rec := g.get("/takes/"+takeID+"/trace", true)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.NotContains(t, body, "<html")
	for _, want := range []string{
		"Prompt version", "v1", // the Prompt version and its Source Shot Spec
		"World State version", `>v1<`, // the World State the spec was planned against
		"Model", "References", "Mio", // the References the model received
		"Output", "Judge results", "Repair history",
		"A woman turns and runs", // the Source Shot Spec snapshot
		"quality_policy",         // the Model parameters
	} {
		assert.Contains(t, body, want)
	}
	assert.Contains(t, body, "/projects/"+g.project+"/assets/", "the Output links to the video")

	assert.Equal(t, http.StatusNotFound, g.get("/takes/nope/trace", true).Code)
}
