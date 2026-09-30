package router_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var branchLocationRe = regexp.MustCompile(`^/branches/([0-9a-f-]{36})$`)

// branchApp is a Project with one Scene and one Shot.
func branchApp(t *testing.T) (a *app, project, shot string) {
	t.Helper()
	a = newApp(t)
	project = a.createProject("美容液の縦型15秒CMを作りたい")
	rec := a.form(http.MethodPost, "/projects/"+project+"/scenes", url.Values{"title": {"Street"}})
	m := sceneIDRe.FindStringSubmatch(rec.Body.String())
	require.NotNil(t, m)
	rec = a.form(http.MethodPost, "/scenes/"+m[1]+"/shots", url.Values{"objective": {"走り出す"}, "duration_seconds": {"4"}})
	require.Equal(t, http.StatusCreated, rec.Code)
	m = shotIDRe.FindStringSubmatch(rec.Body.String())
	require.NotNil(t, m)
	return a, project, m[1]
}

func TestBranch_PostSavesTheBranchPointAndRedirectsToTheBranch(t *testing.T) {
	a, project, shot := branchApp(t)

	rec := a.form(http.MethodPost, "/shots/"+shot+"/branch", url.Values{"point_type": {"frame"}, "offset_seconds": {"1.5"}, "name": {"逃げる"}})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	loc := rec.Header().Get("HX-Redirect")
	m := branchLocationRe.FindStringSubmatch(loc)
	require.NotNil(t, m, loc)
	id := m[1]

	page := a.get(loc, false)
	require.Equal(t, http.StatusOK, page.Code)
	assert.Contains(t, page.Body.String(), "<html")
	assert.Contains(t, page.Body.String(), `<mam-branch-tree api-url="/api/v1/projects/`+project+`/branches"`)
	assert.Contains(t, page.Body.String(), "逃げる")
	assert.Contains(t, page.Body.String(), "1.5 秒地点", "the Frame Branch Point is shown")
	assert.Contains(t, page.Body.String(), `href="/projects/`+project+`/world-state?branch_id=`+id+`"`, "the way to the World State of the Branch")

	frag := a.get("/branches/"+id, true)
	assert.NotContains(t, frag.Body.String(), "<html", "HTMX (branch-selected) gets the detail only")
	assert.Contains(t, frag.Body.String(), `data-branch-detail="`+id+`"`)

	list := a.get("/projects/"+project+"/branches", false)
	require.Equal(t, http.StatusOK, list.Code)
	assert.Contains(t, list.Body.String(), `hx-post="/shots/`+shot+`/branch"`, "a Branch can be made at every Shot")

	ws := a.get("/projects/"+project+"/world-state?branch_id="+id, false)
	require.Equal(t, http.StatusOK, ws.Code)
	assert.Contains(t, ws.Body.String(), "Branch「逃げる」の World State", "the World State page addresses the Branch")
}

func TestBranch_PostRejectsBadInput(t *testing.T) {
	a, _, shot := branchApp(t)

	assert.Equal(t, http.StatusNotFound, a.form(http.MethodPost, "/shots/nope/branch", url.Values{}).Code)
	assert.Equal(t, http.StatusUnprocessableEntity, a.form(http.MethodPost, "/shots/"+shot+"/branch", url.Values{"point_type": {"moon"}}).Code)
	assert.Equal(t, http.StatusUnprocessableEntity, a.form(http.MethodPost, "/shots/"+shot+"/branch", url.Values{"point_type": {"frame"}}).Code, "a frame needs an offset")
	assert.Equal(t, http.StatusUnprocessableEntity, a.form(http.MethodPost, "/shots/"+shot+"/branch", url.Values{"point_type": {"frame"}, "offset_seconds": {"99"}}).Code, "past the end of the Shot")
	assert.Equal(t, http.StatusNotFound, a.get("/branches/nope", false).Code)
	assert.Equal(t, http.StatusNotFound, a.get("/projects/nope/branches", false).Code)
}

func TestBranch_PutRenamesAndMovesTheWorldStateReferenceIntoBranchVersions(t *testing.T) {
	a, _, shot := branchApp(t)
	rec := a.form(http.MethodPost, "/shots/"+shot+"/branch", url.Values{"name": {"A"}})
	require.Equal(t, http.StatusCreated, rec.Code)
	id := branchLocationRe.FindStringSubmatch(rec.Header().Get("HX-Redirect"))[1]

	rec = a.form(http.MethodPut, "/branches/"+id, url.Values{"name": {"B"}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "B")
	assert.Equal(t, "branches-changed", rec.Header().Get("HX-Trigger"), "the tree redraws itself")
	assert.Equal(t, 2, a.branchVersions(id), "the rename is appended")

	assert.Equal(t, http.StatusUnprocessableEntity, a.form(http.MethodPut, "/branches/"+id, url.Values{}).Code)
	assert.Equal(t, http.StatusUnprocessableEntity, a.form(http.MethodPut, "/branches/"+id, url.Values{"world_state_version": {"7"}}).Code, "a version the Branch does not have")
	assert.Equal(t, http.StatusNotFound, a.form(http.MethodPut, "/branches/nope", url.Values{"name": {"x"}}).Code)
	assert.Equal(t, 2, a.branchVersions(id), "a refused change appends nothing")
}

func (a *app) branchVersions(id string) int {
	a.t.Helper()
	var n int
	require.NoError(a.t, a.db.Reader().QueryRow(`SELECT COUNT(*) FROM branch_version WHERE branch_id = ?`, id).Scan(&n))
	return n
}

func TestBranch_APIReturnsTheTreeWithNestedChildrenAndUrls(t *testing.T) {
	a, project, shot := branchApp(t)
	rec := a.form(http.MethodPost, "/shots/"+shot+"/branch", url.Values{"name": {"A"}, "point_type": {"scene"}})
	require.Equal(t, http.StatusCreated, rec.Code)
	id := branchLocationRe.FindStringSubmatch(rec.Header().Get("HX-Redirect"))[1]

	rec = a.get("/api/v1/projects/"+project+"/branches", false)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body struct {
		ProjectID string `json:"project_id"`
		Branches  []struct {
			ID              string  `json:"id"`
			Name            string  `json:"name"`
			BranchPointType string  `json:"branch_point_type"`
			SceneNumber     int     `json:"scene_number"`
			SnapshotVersion int     `json:"snapshot_version"`
			DetailURL       string  `json:"detail_url"`
			WorldStateURL   string  `json:"world_state_url"`
			Children        []any   `json:"children"`
			OffsetSeconds   float64 `json:"offset_seconds"`
		} `json:"branches"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Branches, 1)
	b := body.Branches[0]
	assert.Equal(t, project, body.ProjectID)
	assert.Equal(t, id, b.ID)
	assert.Equal(t, "scene", b.BranchPointType)
	assert.Equal(t, 1, b.SceneNumber)
	assert.Equal(t, 1, b.SnapshotVersion)
	assert.Equal(t, "/branches/"+id, b.DetailURL)
	assert.Equal(t, "/projects/"+project+"/world-state?branch_id="+id, b.WorldStateURL)
	assert.NotNil(t, b.Children, "children is an array, never null")

	assert.Equal(t, http.StatusNotFound, a.get("/api/v1/projects/nope/branches", false).Code)
}
