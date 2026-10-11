package router_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

func TestSettingsPage_ShowsTemplatePolicyBudgetCostAndStorage(t *testing.T) {
	a := newApp(t)
	id := a.createProject("美容液のCMを作りたい")

	rec := a.get("/projects/"+id+"/settings", false)
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "<html")
	for _, want := range []string{
		`id="production-template-form"`, `hx-put="/projects/` + id + `/production-template"`, "PRODUCT / CM", "DRAMA", "ANIME", "SNS / SHORT VIDEO",
		"Product Accuracy",
		`id="policy-form"`, `hx-put="/projects/` + id + `/quality-policy"`, `value="balanced" selected`, `name="max_retries"`, `name="min_axis_score"`,
		`id="budget-form"`, `name="project_budget"`, `name="shot_budget"`,
		`id="cost-summary-panel"`, `hx-get="/projects/` + id + `/cost-summary"`,
		`id="storage-panel"`, "ディスクの空き容量",
	} {
		assert.Contains(t, body, want)
	}
	assertChecked(t, body, "product_cm", "the current template is selected")

	frag := a.get("/projects/"+id+"/settings", true).Body.String()
	assert.NotContains(t, frag, "<html")
	assert.Contains(t, frag, `id="policy-form"`)

	assert.Equal(t, http.StatusNotFound, a.get("/projects/missing/settings", false).Code)
	assert.Contains(t, a.get("/projects/"+id, false).Body.String(), `href="/projects/`+id+`/settings"`, "the workspace links to the settings")
}

func TestProductionTemplate_PutSwitchesTheTemplateAndRejectsUnknownOnes(t *testing.T) {
	a := newApp(t)
	id := a.createProject("ドラマを作りたい")

	rec := a.form(http.MethodPut, "/projects/"+id+"/production-template", url.Values{"production_template": {"drama"}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assertChecked(t, rec.Body.String(), "drama")
	var got string
	require.NoError(t, a.db.Reader().QueryRow(`SELECT production_template FROM project WHERE id = ?`, id).Scan(&got))
	assert.Equal(t, "drama", got)

	rec = a.form(http.MethodPut, "/projects/"+id+"/production-template", url.Values{"production_template": {"movie"}})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, "none", rec.Header().Get("HX-Reswap"))
	assert.Equal(t, http.StatusNotFound, a.form(http.MethodPut, "/projects/missing/production-template", url.Values{"production_template": {"drama"}}).Code)
}

func TestQualityPolicy_PutFromTheComposerSwitchesOnlyThePolicyAndTheFormSetsTheLimits(t *testing.T) {
	a := newApp(t)
	id := a.createProject("CM")

	// The composer sends only the policy: the overrides stay.
	require.Equal(t, http.StatusOK, a.form(http.MethodPut, "/projects/"+id+"/quality-policy", url.Values{
		"quality_policy": {"fast"}, "max_retries": {"3"}, "max_generation_seconds": {""}, "min_acceptance_score": {"88"}, "min_axis_score": {""},
	}).Code)
	rec := a.form(http.MethodPut, "/projects/"+id+"/quality-policy", url.Values{"quality_policy": {"max_quality"}})
	assert.Equal(t, http.StatusNoContent, rec.Code)

	var quality string
	var retries, acceptance int
	require.NoError(t, a.db.Reader().QueryRow(`SELECT p.quality_policy, pp.max_retries, pp.min_acceptance_score FROM project p JOIN project_policy pp ON pp.project_id = p.id WHERE p.id = ?`, id).
		Scan(&quality, &retries, &acceptance))
	assert.Equal(t, "max_quality", quality)
	assert.Equal(t, 3, retries)
	assert.Equal(t, 88, acceptance)
}

func TestQualityPolicy_InvalidLimitsReRenderTheFormWithTheInputAndTheError(t *testing.T) {
	a := newApp(t)
	id := a.createProject("CM")

	rec := a.form(http.MethodPut, "/projects/"+id+"/quality-policy", url.Values{
		"quality_policy": {"fast"}, "max_retries": {"99"}, "max_generation_seconds": {""}, "min_acceptance_score": {""}, "min_axis_score": {""},
	})
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, `id="policy-form"`)
	assert.Contains(t, body, `value="99"`, "the user's input is kept")
	assert.Contains(t, body, "Max retries は 0〜10")
	assert.Contains(t, body, `value="fast" selected`)

	var quality string
	require.NoError(t, a.db.Reader().QueryRow(`SELECT quality_policy FROM project WHERE id = ?`, id).Scan(&quality))
	assert.Equal(t, "balanced", quality, "nothing was saved")

	assert.Equal(t, http.StatusUnprocessableEntity, a.form(http.MethodPut, "/projects/"+id+"/quality-policy", url.Values{"quality_policy": {"slow"}}).Code)
	assert.Equal(t, http.StatusNotFound, a.form(http.MethodPut, "/projects/missing/quality-policy", url.Values{"quality_policy": {"fast"}}).Code)
}

func TestBudget_PutSavesCeilingsAndInvalidInputReRendersTheForm(t *testing.T) {
	a := newApp(t)
	id := a.createProject("CM")

	rec := a.form(http.MethodPut, "/projects/"+id+"/budget", url.Values{"project_budget": {"50"}, "shot_budget": {"2.5"}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `value="50"`)
	assert.Contains(t, rec.Body.String(), `value="2.5"`)
	var project, shot float64
	require.NoError(t, a.db.Reader().QueryRow(`SELECT project_budget_ceiling, shot_budget_ceiling FROM project WHERE id = ?`, id).Scan(&project, &shot))
	assert.InDelta(t, 50, project, 1e-9)
	assert.InDelta(t, 2.5, shot, 1e-9)

	rec = a.form(http.MethodPut, "/projects/"+id+"/budget", url.Values{"project_budget": {"abc"}, "shot_budget": {"1"}})
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), `value="abc"`)
	assert.Contains(t, rec.Body.String(), "Project budget は")
	require.NoError(t, a.db.Reader().QueryRow(`SELECT project_budget_ceiling FROM project WHERE id = ?`, id).Scan(&project))
	assert.InDelta(t, 50, project, 1e-9, "a rejected form saves nothing")

	rec = a.form(http.MethodPut, "/projects/"+id+"/budget", url.Values{})
	require.Equal(t, http.StatusOK, rec.Code)
	var n int
	require.NoError(t, a.db.Reader().QueryRow(`SELECT COUNT(*) FROM project WHERE id = ? AND project_budget_ceiling IS NULL AND shot_budget_ceiling IS NULL`, id).Scan(&n))
	assert.Equal(t, 1, n, "empty fields remove the ceilings")
	assert.Equal(t, http.StatusNotFound, a.form(http.MethodPut, "/projects/missing/budget", url.Values{}).Code)
}

func TestCostSummary_ShowsCostByShotAfterGenerationAndTheBudgetBlocksWith402(t *testing.T) {
	g := newGenApp(t, true)

	before := g.get("/projects/"+g.project+"/cost-summary", true)
	require.Equal(t, http.StatusOK, before.Code)
	assert.Contains(t, before.Body.String(), `id="cost-summary-panel"`)
	assert.Contains(t, before.Body.String(), "Scene 1: Street")
	assert.Contains(t, before.Body.String(), "Shot 1: 走り出す", "a Shot is listed before anything was spent")

	require.Equal(t, http.StatusOK, g.form(http.MethodPut, "/projects/"+g.project+"/budget", url.Values{"project_budget": {"10"}, "shot_budget": {"0.01"}}).Code)
	rec := g.form(http.MethodPost, "/shots/"+g.shotID+"/generate", nil)
	assert.Equal(t, http.StatusPaymentRequired, rec.Code, "the Shot budget cannot afford a Take")
	assert.Contains(t, rec.Body.String(), "予算上限")

	require.Equal(t, http.StatusOK, g.form(http.MethodPut, "/projects/"+g.project+"/budget", url.Values{"project_budget": {"10"}}).Code)
	g.generateTakes()

	after := g.get("/projects/"+g.project+"/cost-summary", true).Body.String()
	assert.Contains(t, after, `data-testid="project-estimated">$0.960<`, "3 Takes x 4 s x 0.08 USD/s")
	assert.Contains(t, after, `data-testid="project-actual">-<`, "the fake vendor reports no cost")
	assert.Contains(t, after, "上限: $10.00")
	assert.Contains(t, g.get("/projects/"+g.project+"/settings", false).Body.String(), `data-testid="project-estimated">$0.960<`)

	assert.Equal(t, http.StatusNotFound, g.get("/projects/missing/cost-summary", true).Code)
}

func TestStorage_ArchiveEndpointsFreeTakesAndRefuseTheSelectedOne(t *testing.T) {
	g := newGenApp(t, true)
	ids := g.generateTakes()
	selectTake(t, g, ids[0])

	panel := g.get("/projects/"+g.project+"/settings", false).Body.String()
	assert.Contains(t, panel, `data-testid="unneeded">2 件`)
	assert.Contains(t, panel, `hx-post="/projects/`+g.project+`/storage/archive-unneeded"`)

	assert.Equal(t, http.StatusConflict, g.form(http.MethodPost, "/projects/"+g.project+"/takes/"+ids[0]+"/archive", nil).Code, "the Selected Take")
	rec := g.form(http.MethodPost, "/projects/"+g.project+"/takes/"+ids[1]+"/archive", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `data-testid="unneeded">1 件`)

	rec = g.form(http.MethodPost, "/projects/"+g.project+"/storage/archive-unneeded", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `data-testid="unneeded">0 件`)
	assert.NotContains(t, rec.Body.String(), `hx-post="/projects/`+g.project+`/storage/archive-unneeded"`)

	assert.Equal(t, http.StatusNotFound, g.form(http.MethodPost, "/projects/"+g.project+"/takes/missing/archive", nil).Code)
	assert.Equal(t, http.StatusNotFound, g.form(http.MethodPost, "/projects/missing/storage/archive-unneeded", nil).Code)
}

func TestStorage_OffersArchiveOnlyForTakesTheServerWouldArchive(t *testing.T) {
	g := newGenApp(t, true)
	ids := g.generateTakes()
	used, chosen, free := ids[0], ids[1], ids[2]

	// The Rough Cut used Take A, then Take B was chosen: A is no longer Selected, but the Edit
	// Timeline still points at it.
	_, err := repository.NewEditorRepository(g.db).SaveEditTimeline(context.Background(), g.project, "", "",
		domain.EditTimeline{Clips: []domain.EditClip{{ShotID: g.shotID, TakeID: used, Out: 1}}})
	require.NoError(t, err)
	selectTake(t, g, chosen)

	archive := func(id string) string { return `hx-post="/projects/` + g.project + `/takes/` + id + `/archive"` }
	panel := g.get("/projects/"+g.project+"/settings", false).Body.String()
	assert.NotContains(t, panel, archive(used), "a Take the Edit Timeline uses cannot be archived")
	assert.NotContains(t, panel, archive(chosen), "the Selected Take cannot be archived")
	assert.Contains(t, panel, archive(free))
	assert.Equal(t, 2, strings.Count(panel, `data-testid="take-in-use"`), "the two refused rows say why")

	rec := g.form(http.MethodPost, "/projects/"+g.project+"/takes/"+used+"/archive", nil)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "タイムライン", "the 409 names the reason, not the chat approval wording")
	assert.NotContains(t, rec.Body.String(), "すでに応答済み")
}

func TestStorage_ATakeWhoseStoredPathLeavesTheAssetsDirectoryIsNeitherServedNorDeleted(t *testing.T) {
	g := newGenApp(t, true)
	ids := g.generateTakes()
	selectTake(t, g, ids[0])

	outside := filepath.Join(filepath.Dir(g.assets), "outside")
	require.NoError(t, os.MkdirAll(outside, 0o750))
	for i, id := range ids[1:] {
		name := "keep" + strconv.Itoa(i) + ".mp4"
		require.NoError(t, os.WriteFile(filepath.Join(outside, name), []byte("precious"), 0o600))
		g.execSQL(`UPDATE asset SET file_path = ? WHERE file_path = (SELECT file_path FROM take WHERE id = ?)`, "../outside/"+name, id)
		g.execSQL(`UPDATE take SET file_path = ? WHERE id = ?`, "../outside/"+name, id)
	}

	assert.Equal(t, http.StatusNotFound, g.get("/takes/"+ids[1]+"/video", false).Code, "a file outside the assets directory is not served")

	require.Equal(t, http.StatusOK, g.form(http.MethodPost, "/projects/"+g.project+"/takes/"+ids[1]+"/archive", nil).Code)
	require.Equal(t, http.StatusOK, g.form(http.MethodPost, "/projects/"+g.project+"/storage/archive-unneeded", nil).Code)
	for i := range 2 {
		_, err := os.Stat(filepath.Join(outside, "keep"+strconv.Itoa(i)+".mp4"))
		assert.NoError(t, err, "the archive never removes a file outside the assets directory")
	}
}

// selectTake makes the Take the Selected Take of its Shot, as the Take system does.
func selectTake(t *testing.T, g *genApp, takeID string) {
	t.Helper()
	require.NoError(t, g.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE take SET status = 'selected' WHERE id = ?`, takeID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE shot SET selected_take_id = ? WHERE id = ?`, takeID, g.shotID)
		return err
	}))
}
