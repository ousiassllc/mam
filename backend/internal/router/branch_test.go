package router_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
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
	_, shot = a.createShot(project)
	return a, project, shot
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
	assert.Contains(t, page.Body.String(), `<nav id="branch-tree"`)
	assert.Contains(t, page.Body.String(), `hx-get="/branches/`+id+`"`)
	assert.Contains(t, page.Body.String(), "逃げる")
	assert.Contains(t, page.Body.String(), "1.5 秒地点", "the Frame Branch Point is shown")
	assert.Contains(t, page.Body.String(), `href="/projects/`+project+`/world-state?branch_id=`+id+`"`, "the way to the World State of the Branch")

	frag := a.get("/branches/"+id, true)
	assert.NotContains(t, frag.Body.String(), "<html", "HTMX gets the detail and the tree only")
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
	assert.Contains(t, rec.Body.String(), `hx-swap-oob="true"`, "the tree redraws itself with the new name")
	assert.Contains(t, rec.Body.String(), `data-branch="`+id+`">B（`, "the tree shows the new name")
	assert.Empty(t, rec.Header().Get("HX-Trigger"))
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

func TestBranch_HTMXSelectionRedrawsTheServerRenderedTreeWithTheBranchSelected(t *testing.T) {
	a, project, shot := branchApp(t)
	first := a.form(http.MethodPost, "/shots/"+shot+"/branch", url.Values{"name": {"A"}, "point_type": {"scene"}})
	require.Equal(t, http.StatusCreated, first.Code)
	idA := branchLocationRe.FindStringSubmatch(first.Header().Get("HX-Redirect"))[1]
	second := a.form(http.MethodPost, "/shots/"+shot+"/branch", url.Values{"name": {"B"}})
	require.Equal(t, http.StatusCreated, second.Code)
	idB := branchLocationRe.FindStringSubmatch(second.Header().Get("HX-Redirect"))[1]

	list := a.get("/projects/"+project+"/branches", true)
	require.Equal(t, http.StatusOK, list.Code)
	for _, id := range []string{idA, idB} {
		assert.Contains(t, list.Body.String(), `hx-get="/branches/`+id+`"`, "every Branch loads its detail on choosing")
	}
	assert.Contains(t, list.Body.String(), `hx-target="#branch-detail"`)
	assert.NotContains(t, list.Body.String(), `aria-selected="true"`, "nothing is selected on the list")
	assert.NotContains(t, list.Body.String(), "<mam-branch-tree")

	rec := a.get("/branches/"+idA, true)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `data-branch-detail="`+idA+`"`)
	assert.Contains(t, rec.Body.String(), `hx-swap-oob="true"`, "the tree is redrawn along with the detail")
	assert.Equal(t, 1, strings.Count(rec.Body.String(), `aria-selected="true"`), "only the chosen Branch is selected")
	assert.Regexp(t, `aria-selected="true"[^>]*>\s*<a[^>]*data-branch="`+idA+`"`, rec.Body.String())

	assert.Equal(t, http.StatusNotFound, a.get("/api/v1/projects/"+project+"/branches", false).Code, "the tree has no JSON twin")
}

func TestBranch_TheWorkspaceMenuAndTheShotInspectorLeadToTheBranchPage(t *testing.T) {
	a, project, shot := branchApp(t)
	list := `href="/projects/` + project + `/branches"`
	assert.Contains(t, a.get("/projects/"+project, false).Body.String(), list, "⋯ 補助ビュー")
	assert.Contains(t, a.get("/shots/"+shot, true).Body.String(), list, "Inspector of a Shot")
}
