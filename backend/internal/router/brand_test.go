package router_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

func TestBrandConstraints_PostRegistersEveryTypeAndDeleteRemovesIt(t *testing.T) {
	a := newApp(t)
	id := a.createProject("CM")

	for _, typ := range domain.BrandConstraintTypes() {
		rec := a.form(http.MethodPost, "/projects/"+id+"/brand-constraints", url.Values{"type": {string(typ)}, "level": {"advisory"}, "text": {"ルール " + string(typ)}})
		require.Equal(t, http.StatusOK, rec.Code, "%s: %s", typ, rec.Body.String())
		assert.Contains(t, rec.Body.String(), `id="brand-constraint-list"`)
	}
	var n int
	require.NoError(t, a.db.Reader().QueryRow(`SELECT COUNT(DISTINCT type) FROM brand_constraint WHERE project_id = ?`, id).Scan(&n))
	assert.Equal(t, 8, n)

	// Without a level the strict one (LOCK) is registered.
	rec := a.form(http.MethodPost, "/projects/"+id+"/brand-constraints", url.Values{"type": {"forbidden_expression"}, "text": {"最安値\n業界No.1"}})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "最安値")
	var cid, level string
	require.NoError(t, a.db.Reader().QueryRow(`SELECT id, enforcement_level FROM brand_constraint WHERE value LIKE '%最安値%'`).Scan(&cid, &level))
	assert.Equal(t, "lock", level)

	rec = a.form(http.MethodDelete, "/brand-constraints/"+cid, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "最安値")
	require.NoError(t, a.db.Reader().QueryRow(`SELECT COUNT(*) FROM brand_constraint WHERE id = ?`, cid).Scan(&n))
	assert.Zero(t, n)
	assert.Equal(t, http.StatusNotFound, a.form(http.MethodDelete, "/brand-constraints/"+cid, nil).Code)
}

func TestBrandConstraints_InvalidInputReRendersTheListWithTheInputAndTheError(t *testing.T) {
	a := newApp(t)
	id := a.createProject("CM")

	tests := []struct {
		name string
		form url.Values
		want string
	}{
		{"unknown type", url.Values{"type": {"tone"}, "level": {"lock"}, "text": {"x"}}, "種類が正しくありません"},
		{"unknown level", url.Values{"type": {"logo_rule"}, "level": {"strict"}, "text": {"x"}}, "強制レベルが正しくありません"},
		{"empty text", url.Values{"type": {"logo_rule"}, "level": {"lock"}, "text": {"  "}}, "内容を入力してください"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := a.form(http.MethodPost, "/projects/"+id+"/brand-constraints", tc.form)
			require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
			assert.Contains(t, rec.Body.String(), tc.want)
			assert.Contains(t, rec.Body.String(), `id="brand-constraint-list"`)
		})
	}
	var n int
	require.NoError(t, a.db.Reader().QueryRow(`SELECT COUNT(*) FROM brand_constraint`).Scan(&n))
	assert.Zero(t, n, "nothing was saved")

	assert.Equal(t, http.StatusNotFound, a.form(http.MethodPost, "/projects/missing/brand-constraints", url.Values{"type": {"logo_rule"}, "level": {"lock"}, "text": {"x"}}).Code)
}

func TestSettingsPage_ListsTheBrandConstraints(t *testing.T) {
	a := newApp(t)
	id := a.createProject("CM")
	require.Equal(t, http.StatusOK, a.form(http.MethodPost, "/projects/"+id+"/brand-constraints", url.Values{"type": {"required_disclaimer"}, "level": {"lock"}, "text": {"効果には個人差があります"}}).Code)

	body := a.get("/projects/"+id+"/settings", false).Body.String()
	for _, want := range []string{`id="brand-constraint-list"`, "効果には個人差があります", "必須の免責表記", `hx-post="/projects/` + id + `/brand-constraints"`, `hx-delete="/brand-constraints/`} {
		assert.Contains(t, body, want)
	}
}

func TestGenerate_ADirectionThatBreaksALockConstraintAnswers403AndAdvisoryDoesNot(t *testing.T) {
	g := newGenApp(t, true)
	require.Equal(t, http.StatusOK, g.form(http.MethodPost, "/projects/"+g.project+"/brand-constraints", url.Values{"type": {"forbidden_expression"}, "level": {"advisory"}, "text": {"激安"}}).Code)
	require.Equal(t, http.StatusOK, g.form(http.MethodPost, "/projects/"+g.project+"/brand-constraints", url.Values{"type": {"forbidden_expression"}, "level": {"lock"}, "text": {"最安値"}}).Code)

	rec := g.form(http.MethodPost, "/shots/"+g.shotID+"/generate", url.Values{"additional_instruction": {"最安値をうたって"}})
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "最安値")
	assert.Equal(t, "none", rec.Header().Get("HX-Reswap"))
	var takes int
	require.NoError(t, g.db.Reader().QueryRow(`SELECT COUNT(*) FROM take`).Scan(&takes))
	assert.Zero(t, takes)

	rec = g.form(http.MethodPost, "/shots/"+g.shotID+"/generate", url.Values{"additional_instruction": {"激安感を出して"}})
	assert.Equal(t, http.StatusAccepted, rec.Code, "an Advisory constraint only warns")
	g.waitTakes(3)
}

func (j *judgeApp) addBrand(id, typ, level, text string) {
	j.t.Helper()
	j.execSQL(`INSERT INTO brand_constraint (id, project_id, type, enforcement_level, value) VALUES (?, ?, ?, ?, json_object('text', ?))`, id, j.project, typ, level, text)
}

func TestJudge_ABrokenLockBrandConstraintFailsEveryTakeWhateverTheModelSays(t *testing.T) {
	// The model gives a confident pass with only a low severity note that names the constraint.
	j := newJudgeApp(t, `{"result":"pass","overall_confidence":0.97,"issues":[{"type":"logo","severity":"low","brand_constraint_id":"brand-lock","description":"logo is warped"}]}`,
		0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95)
	j.addBrand("brand-lock", "logo_rule", "lock", "Never distort the logo")
	ids := j.generateAndJudge()

	for _, id := range ids {
		code, body := j.result(id)
		require.Equal(t, http.StatusOK, code)
		assert.Equal(t, "fail", body.Result)
		require.Len(t, body.Issues, 1)
		assert.Equal(t, "brand_constraint_violation", body.Issues[0].Type)
		assert.Equal(t, "brand-lock", body.Issues[0].BrandID)
		assert.True(t, body.Issues[0].LockViolation)
	}
}

func TestJudge_ABrokenAdvisoryBrandConstraintLeavesTheTakePassing(t *testing.T) {
	j := newJudgeApp(t, `{"result":"pass","overall_confidence":0.97,"issues":[{"type":"hook","severity":"high","brand_constraint_id":"brand-adv","description":"weak hook"}]}`,
		0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95)
	j.addBrand("brand-adv", "platform_rule", "advisory", "Hook in the first 2 seconds")
	ids := j.generateAndJudge()

	code, body := j.result(ids[0])
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "pass", body.Result)
	require.Len(t, body.Issues, 1)
	assert.False(t, body.Issues[0].LockViolation)
	assert.Equal(t, "low", body.Issues[0].Severity)
}

func TestGenerate_APlannedSpecThatBreaksALockConstraintIs403BeforeTheJob(t *testing.T) {
	// The Prompt Planner's spec says "A woman turns and runs"; running is forbidden.
	j := newJudgeApp(t, goodVerdict, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95)
	j.addBrand("brand-lock", "forbidden_expression", "lock", "runs")

	rec := j.form(http.MethodPost, "/shots/"+j.shotID+"/generate", nil)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "runs")
	assert.Empty(t, j.takeIDs(), "nothing is generated from a spec that breaks a LOCK constraint")
	assert.Zero(t, j.count(`SELECT COUNT(*) FROM job`), "the job is refused before it is queued")
	var state string
	require.NoError(t, j.db.Reader().QueryRow(`SELECT state FROM shot WHERE id = ?`, j.shotID).Scan(&state))
	assert.Equal(t, "PLANNING", state)

	rec = j.form(http.MethodPost, "/shots/"+j.shotID+"/generate/candidates", nil)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	assert.Zero(t, j.count(`SELECT COUNT(*) FROM job`))
	assert.Empty(t, j.takeIDs())
}

func TestGenerate_AnAdvisoryExpressionInThePlannedSpecDoesNot403(t *testing.T) {
	j := newJudgeApp(t, goodVerdict, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95)
	j.addBrand("brand-adv", "forbidden_expression", "advisory", "runs")

	rec := j.form(http.MethodPost, "/shots/"+j.shotID+"/generate", nil)
	assert.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
}

func TestDirectorCommand_LockBrandViolationIs403BeforeTheJob(t *testing.T) {
	j := newJudgeApp(t, goodVerdict, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95)
	j.addBrand("brand-lock", "forbidden_expression", "lock", "最安値")
	j.addBrand("brand-adv", "forbidden_expression", "advisory", "激安")
	j.llm.command = `{"operations":[{"type":"regenerate","changes":["acting"],"instruction":"Make her nervous."}]}`
	before := j.count(`SELECT COUNT(*) FROM chat_message`)

	rec := j.form(http.MethodPost, "/shots/"+j.shotID+"/director-command", url.Values{"content": {"最安値をうたって"}})
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "最安値")
	assert.Zero(t, j.count(`SELECT COUNT(*) FROM job`))
	assert.Empty(t, j.takeIDs())
	assert.Equal(t, before, j.count(`SELECT COUNT(*) FROM chat_message`), "a refused direction is not recorded")

	rec = j.form(http.MethodPost, "/shots/"+j.shotID+"/director-command", url.Values{"content": {"激安感を出して"}})
	assert.Equal(t, http.StatusAccepted, rec.Code, "an Advisory constraint only warns")
}

func TestDirectorCommand_APlannedSpecThatBreaksALockConstraintIs403(t *testing.T) {
	j := newJudgeApp(t, goodVerdict, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95)
	j.addBrand("brand-lock", "forbidden_expression", "lock", "runs")
	j.llm.command = `{"operations":[{"type":"regenerate","changes":["acting"],"instruction":"Make her nervous."}]}`

	rec := j.form(http.MethodPost, "/shots/"+j.shotID+"/director-command", url.Values{"content": {"少し慌てさせて"}})
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	assert.Zero(t, j.count(`SELECT COUNT(*) FROM job`))
	assert.Empty(t, j.takeIDs())
}

func TestDirectorCommand_PartialRegenerateThatBreaksALockConstraintIs403(t *testing.T) {
	j := newJudgeApp(t, goodVerdict, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95)
	require.NotEmpty(t, j.generateAndJudge())
	j.addBrand("brand-lock", "forbidden_expression", "lock", "runs")
	j.llm.command = partialCommand
	jobs := j.count(`SELECT COUNT(*) FROM job`)
	takes := len(j.takeIDs())

	rec := j.form(http.MethodPost, "/shots/"+j.shotID+"/director-command", url.Values{"content": {"1〜2秒目だけ直して"}})

	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	assert.Equal(t, jobs, j.count(`SELECT COUNT(*) FROM job`))
	assert.Len(t, j.takeIDs(), takes)
}

func TestChat_RegenerateThatBreaksALockConstraintIs403(t *testing.T) {
	j := newJudgeApp(t, goodVerdict, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95)
	j.addBrand("brand-lock", "forbidden_expression", "lock", "最安値")
	j.execSQL(`UPDATE project SET mode = 'director', state = 'GENERATING' WHERE id = ?`, j.project)
	j.llm.command = `{"operations":[{"type":"regenerate","changes":["acting"],"instruction":"Make her nervous."}]}`

	rec := j.form(http.MethodPost, "/projects/"+j.project+"/messages", url.Values{"content": {"最安値をうたって"}})
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	assert.Zero(t, j.count(`SELECT COUNT(*) FROM job`))
	assert.Empty(t, j.takeIDs())
}

func TestGenerate_AnExistingSpecThatBreaksALockConstraintIs403BeforeTheJob(t *testing.T) {
	j := newJudgeApp(t, goodVerdict, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95)
	require.Equal(t, http.StatusAccepted, j.form(http.MethodPost, "/shots/"+j.shotID+"/generate", nil).Code)
	j.waitTakes(3)
	j.addBrand("brand-lock", "forbidden_expression", "lock", "runs")
	jobs := j.count(`SELECT COUNT(*) FROM job WHERE kind = 'generate'`)

	rec := j.form(http.MethodPost, "/shots/"+j.shotID+"/generate", nil)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	assert.Equal(t, jobs, j.count(`SELECT COUNT(*) FROM job WHERE kind = 'generate'`), "a retake of a forbidden spec is not queued")
	assert.Len(t, j.takeIDs(), 3)
}

// editorToAudio takes the editor flow up to the point where Final QC can run: two script lines
// ("朝が来た" narrated, "行こう" spoken), a Rough Cut, a passing Editor Judge and the mixed audio.
func editorToAudio(t *testing.T) *editorApp {
	t.Helper()
	e := newEditorApp(t)
	e.setState("VISUAL_DESIGN")
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/takes/t1/select", nil).Code)
	doc := domain.ScriptDoc{Script: domain.Script{Scenes: []domain.ScriptScene{{Title: "Street", Dialogue: []string{"ナレーション: 朝が来た", "ユイ: 行こう"}}}}}
	_, err := repository.NewPlanRepository(e.db).SaveScript(context.Background(), e.project, doc)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, e.roughCut().Code)
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/edit-timelines/"+e.timeline().ID+"/judge", nil).Code)
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/projects/"+e.project+"/audio/generate", nil).Code)
	require.Equal(t, "AUDIO", e.state())
	return e
}

func (e *editorApp) addBrand(typ, level, text string) {
	e.t.Helper()
	rec := e.form(http.MethodPost, "/projects/"+e.project+"/brand-constraints", url.Values{"type": {typ}, "level": {level}, "text": {text}})
	require.Equal(e.t, http.StatusOK, rec.Code, rec.Body.String())
}

func TestFinalQC_ALockForbiddenExpressionInTheSpokenLinesFailsAndReturnsToAudio(t *testing.T) {
	e := editorToAudio(t)
	e.addBrand("forbidden_expression", "lock", "朝が来た")

	rec := e.form(http.MethodPost, "/projects/"+e.project+"/final-qc", nil)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "AUDIO", e.state(), "the fix is in the lines, so the Project goes back to AUDIO")
	assert.Contains(t, rec.Body.String(), "data-qc-findings")
	assert.Contains(t, rec.Body.String(), "朝が来た")
	assert.Equal(t, http.StatusConflict, e.form(http.MethodPost, "/projects/"+e.project+"/export", nil).Code, "not READY")
}

func TestFinalQC_AnAdvisoryViolationPassesWithTheWarningShown(t *testing.T) {
	e := editorToAudio(t)
	e.addBrand("forbidden_expression", "advisory", "行こう")

	rec := e.form(http.MethodPost, "/projects/"+e.project+"/final-qc", nil)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "READY", e.state())
	assert.Contains(t, rec.Body.String(), "警告")
}

func TestFinalQC_ARequiredDisclaimerThatIsNeverStatedFailsAndOneThatIsPasses(t *testing.T) {
	e := editorToAudio(t)
	e.addBrand("required_disclaimer", "lock", "効果には個人差があります")
	require.Equal(t, "EDITING", func() string {
		e.form(http.MethodPost, "/projects/"+e.project+"/final-qc", nil)
		return e.state()
	}(), "a missing disclaimer sends the Project back to the editor")

	e = editorToAudio(t)
	e.addBrand("required_disclaimer", "lock", "行こう")
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/projects/"+e.project+"/final-qc", nil).Code)
	assert.Equal(t, "READY", e.state(), "a disclaimer that is spoken passes")
}

func (a *app) addConstraint(projectID, typ, level, text string) {
	a.t.Helper()
	rec := a.form(http.MethodPost, "/projects/"+projectID+"/brand-constraints", url.Values{"type": {typ}, "level": {level}, "text": {text}})
	require.Equal(a.t, http.StatusOK, rec.Code, rec.Body.String())
}

func (a *app) constraintTexts(projectID string) []string {
	a.t.Helper()
	rows, err := a.db.Reader().Query(`SELECT json_extract(value, '$.text') FROM brand_constraint WHERE project_id = ? ORDER BY created_at, rowid`, projectID)
	require.NoError(a.t, err)
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var s string
		require.NoError(a.t, rows.Scan(&s))
		out = append(out, s)
	}
	require.NoError(a.t, rows.Err())
	return out
}

func (a *app) saveTemplate(projectID, name string) string {
	a.t.Helper()
	rec := a.form(http.MethodPost, "/projects/"+projectID+"/brand-templates", url.Values{"name": {name}})
	require.Equal(a.t, http.StatusOK, rec.Code, rec.Body.String())
	var id string
	require.NoError(a.t, a.db.Reader().QueryRow(`SELECT id FROM brand_template WHERE name = ?`, name).Scan(&id))
	return id
}

func TestBrandTemplates_SaveApplyToProjectAndEpisodeStayIndependentOfTheTemplate(t *testing.T) {
	a := newApp(t)
	src := a.createProject("CM")
	other := a.createProject("Other CM")
	a.addConstraint(src, "forbidden_expression", "lock", "最安値")
	a.addConstraint(src, "required_disclaimer", "advisory", "効果には個人差があります")

	// The settings screen offers saving; a saved template is listed with its constraints.
	assert.Contains(t, a.get("/projects/"+src+"/settings", false).Body.String(), `hx-post="/projects/`+src+`/brand-templates"`)
	tpl := a.saveTemplate(src, "ブランドA")
	body := a.get("/projects/"+other+"/settings", false).Body.String()
	for _, want := range []string{"ブランドA", "2 件の制約", "最安値", `hx-post="/projects/` + other + `/brand-templates/` + tpl + `/apply"`} {
		assert.Contains(t, body, want)
	}

	// Apply to another Project.
	rec := a.form(http.MethodPost, "/projects/"+other+"/brand-templates/"+tpl+"/apply", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `id="brand-constraint-list"`)
	assert.Contains(t, rec.Body.String(), "最安値")
	assert.Equal(t, []string{"最安値", "効果には個人差があります"}, a.constraintTexts(other))

	// The Project's constraints change independently; the template does not follow.
	var cid string
	require.NoError(t, a.db.Reader().QueryRow(`SELECT id FROM brand_constraint WHERE project_id = ? AND value LIKE '%最安値%'`, other).Scan(&cid))
	require.Equal(t, http.StatusOK, a.form(http.MethodDelete, "/brand-constraints/"+cid, nil).Code)
	a.addConstraint(other, "logo_rule", "lock", "ロゴを歪めない")
	assert.Equal(t, []string{"最安値", "効果には個人差があります"}, a.constraintTexts(src), "the source Project is untouched")
	var n int
	require.NoError(t, a.db.Reader().QueryRow(`SELECT COUNT(*) FROM brand_template_constraint WHERE template_id = ?`, tpl).Scan(&n))
	assert.Equal(t, 2, n, "the template is untouched")

	// A new Project, and a new Episode of a Series, can start from the template.
	rec = a.multipart(http.MethodPost, "/projects", map[string]string{"content": "新しいCM", "brand_template_id": tpl})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	fresh := strings.TrimPrefix(rec.Header().Get("HX-Redirect"), "/projects/")
	assert.Equal(t, []string{"最安値", "効果には個人差があります"}, a.constraintTexts(fresh))
	assert.Contains(t, a.get("/projects", false).Body.String(), `name="brand_template_id"`)

	rec = a.form(http.MethodPost, "/series", url.Values{"name": {"S"}, "from_project_id": {src}})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	seriesID := strings.TrimPrefix(rec.Header().Get("HX-Redirect"), "/series/")
	assert.Contains(t, a.get("/series/"+seriesID, false).Body.String(), `name="brand_template_id"`)
	rec = a.form(http.MethodPost, "/series/"+seriesID+"/episodes", url.Values{"brand_template_id": {tpl}})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	ep := strings.TrimPrefix(rec.Header().Get("HX-Redirect"), "/projects/")
	assert.Equal(t, []string{"最安値", "効果には個人差があります"}, a.constraintTexts(ep))

	// Deleting the template leaves every Project that used it as it is.
	rec = a.form(http.MethodDelete, "/projects/"+src+"/brand-templates/"+tpl, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), `data-testid="brand-template"`)
	assert.Equal(t, []string{"最安値", "効果には個人差があります"}, a.constraintTexts(ep))
	assert.Equal(t, []string{"最安値", "効果には個人差があります"}, a.constraintTexts(fresh))
}

func TestBrandTemplates_InvalidRequestsAreRejected(t *testing.T) {
	a := newApp(t)
	id := a.createProject("CM")

	// A Project without constraints has nothing to save.
	rec := a.form(http.MethodPost, "/projects/"+id+"/brand-templates", url.Values{"name": {"A"}})
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "保存できる制約がありません")
	assert.Contains(t, rec.Body.String(), `id="brand-template-panel"`)

	a.addConstraint(id, "logo_rule", "lock", "ロゴを歪めない")
	rec = a.form(http.MethodPost, "/projects/"+id+"/brand-templates", url.Values{"name": {"  "}})
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "テンプレート名は")

	a.saveTemplate(id, "Dup")
	rec = a.form(http.MethodPost, "/projects/"+id+"/brand-templates", url.Values{"name": {"dup"}})
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "すでにあります")
	assert.Contains(t, rec.Body.String(), `value="dup"`, "the input is kept")

	assert.Equal(t, http.StatusNotFound, a.form(http.MethodPost, "/projects/missing/brand-templates", url.Values{"name": {"X"}}).Code)
	assert.Equal(t, http.StatusNotFound, a.form(http.MethodPost, "/projects/"+id+"/brand-templates/nope/apply", nil).Code)
	assert.Equal(t, http.StatusNotFound, a.form(http.MethodDelete, "/projects/"+id+"/brand-templates/nope", nil).Code)

	// An unknown template on creation is a form error and creates nothing.
	rec = a.multipart(http.MethodPost, "/projects", map[string]string{"content": "別のCM", "brand_template_id": "nope"})
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "見つかりません")
	var n int
	require.NoError(t, a.db.Reader().QueryRow(`SELECT COUNT(*) FROM project`).Scan(&n))
	assert.Equal(t, 1, n)
}
