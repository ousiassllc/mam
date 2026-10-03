package router_test

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// A steady face all along, and a face that is gone from the third frame on (a LOCK violation).
var (
	steadyFaces = []float64{0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95}
	driftFaces  = []float64{0.95, 0.95, 0.2, 0.2, 0.2, 0.2, 0.2, 0.2}
)

func (j *judgeApp) setQuality(policy string) {
	j.t.Helper()
	rec := j.form(http.MethodPut, "/projects/"+j.project+"/quality-policy", url.Values{"quality_policy": {policy}})
	require.Equal(j.t, http.StatusNoContent, rec.Code, rec.Body.String())
}

// candidates starts Prompt Candidate Generation and waits until the Best Take is picked.
func (j *judgeApp) candidates() {
	j.t.Helper()
	rec := j.form(http.MethodPost, "/shots/"+j.shotID+"/generate/candidates", nil)
	require.Equal(j.t, http.StatusAccepted, rec.Code, rec.Body.String())
	require.Eventually(j.t, func() bool { return j.count(`SELECT COUNT(*) FROM take WHERE status = ?`, domain.TakeAIPick) == 1 },
		30*time.Second, 10*time.Millisecond, "the Judge picks the Best Take")
	require.Eventually(j.t, func() bool {
		return j.count(`SELECT COUNT(*) FROM job WHERE status IN ('queued', 'running')`) == 0
	}, 30*time.Second, 10*time.Millisecond)
}

func (j *judgeApp) count(q string, args ...any) int {
	var n int
	require.NoError(j.t, j.db.Reader().QueryRow(q, args...).Scan(&n))
	return n
}

func (j *judgeApp) candidateStrategies() []string {
	j.t.Helper()
	rows, err := j.db.Reader().Query(`SELECT json_extract(model_parameters, '$.candidate_strategy') FROM take ORDER BY created_at, id`)
	require.NoError(j.t, err)
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var s string
		require.NoError(j.t, rows.Scan(&s))
		out = append(out, s)
	}
	return out
}

func TestCandidates_QualityModeMakesEveryStrategyOnRunwayAndTheJudgePicksTheBestTake(t *testing.T) {
	j := newJudgeApp(t, goodVerdict, steadyFaces...)
	j.setQuality("max_quality")
	j.candidates()

	assert.ElementsMatch(t, []string{"stability_first", "acting_first", "dynamic_camera"}, j.candidateStrategies())
	assert.Equal(t, 3, j.count(`SELECT COUNT(*) FROM take WHERE model_name = 'runway-gen-4.5'`), "MAX QUALITY: Runway Gen-4.5 is the primary")
	assert.Equal(t, 3, j.llm.judgeCalls(), "every Candidate is judged")
	assert.Equal(t, 3, j.count(`SELECT COUNT(*) FROM prompt_version`), "each Candidate has a Prompt Version of its own")
}

func TestCandidates_QualityModeGeneratesOnlyTheStrategiesTheBudgetPaysFor(t *testing.T) {
	j := newJudgeApp(t, goodVerdict, steadyFaces...)
	j.setQuality("max_quality")
	// Runway Gen-4.5 at 4s is 0.48 per Take. 1.00 pays for two Takes, not three.
	require.Equal(t, http.StatusOK, j.form(http.MethodPut, "/projects/"+j.project+"/budget", url.Values{"shot_budget": {"1"}}).Code)

	j.candidates()

	assert.ElementsMatch(t, []string{"stability_first", "acting_first"}, j.candidateStrategies())
	assert.Equal(t, 2, j.count(`SELECT COUNT(*) FROM take`))
}

func TestCandidates_QualityModeIs402WhenNoTakeFitsTheBudget(t *testing.T) {
	j := newJudgeApp(t, goodVerdict, steadyFaces...)
	j.setQuality("max_quality")
	require.Equal(t, http.StatusOK, j.form(http.MethodPut, "/projects/"+j.project+"/budget", url.Values{"shot_budget": {"0.01"}}).Code)
	jobs := j.count(`SELECT COUNT(*) FROM job WHERE kind = 'generate'`)

	rec := j.form(http.MethodPost, "/shots/"+j.shotID+"/generate/candidates", nil)

	assert.Equal(t, http.StatusPaymentRequired, rec.Code, rec.Body.String())
	assert.Equal(t, jobs, j.count(`SELECT COUNT(*) FROM job WHERE kind = 'generate'`))
	assert.Zero(t, j.count(`SELECT COUNT(*) FROM take`))
}

func TestCandidates_BalancedModeStopsAtTheFirstCandidateThatPasses(t *testing.T) {
	j := newJudgeApp(t, goodVerdict, steadyFaces...)
	j.candidates()

	assert.Equal(t, []string{"stability_first"}, j.candidateStrategies())
	assert.Equal(t, 1, j.llm.judgeCalls())
}

func TestCandidates_BalancedModeAddsTheNextCandidateWhileTheJudgeFailsAndStopsAtTheRetryLimit(t *testing.T) {
	j := newJudgeApp(t, goodVerdict, driftFaces...)
	j.addMatchingCanonFace(t)
	j.candidates()

	assert.Equal(t, []string{"stability_first", "acting_first", "dynamic_camera"}, j.candidateStrategies(), "1 + Max retries (2) Takes, one per strategy")
}

func TestCandidates_EconomyModeMakesOneTakeAndRetakesOnlyAfterASevereProblem(t *testing.T) {
	ok := newJudgeApp(t, goodVerdict, steadyFaces...)
	ok.setQuality("fast")
	ok.candidates()
	assert.Equal(t, []string{"stability_first"}, ok.candidateStrategies())
	assert.Equal(t, 1, ok.count(`SELECT COUNT(*) FROM take WHERE model_name = 'kling-3.0'`), "FAST: one Take on Kling 3.0")

	bad := newJudgeApp(t, goodVerdict, driftFaces...)
	bad.addMatchingCanonFace(t)
	bad.setQuality("fast")
	bad.candidates()
	assert.Equal(t, []string{"stability_first", "stability_first"}, bad.candidateStrategies(), "a LOCK violation is retaken once (Max retries 1)")
}

func TestCandidates_IsUnavailableWithoutTheJudgeAndUnknownShotIs404(t *testing.T) {
	g := newGenApp(t, true) // no Frames: the Judge is off
	assert.Equal(t, http.StatusServiceUnavailable, g.form(http.MethodPost, "/shots/"+g.shotID+"/generate/candidates", nil).Code)

	j := newJudgeApp(t, goodVerdict, steadyFaces...)
	assert.Equal(t, http.StatusNotFound, j.form(http.MethodPost, "/shots/missing/generate/candidates", nil).Code)
}

func TestCandidates_ALockedPromptRulesOutRewritingTheSpec(t *testing.T) {
	j := newJudgeApp(t, goodVerdict, steadyFaces...)
	j.generateAndJudge() // the Shot has a spec now
	takes := j.count(`SELECT COUNT(*) FROM take`)
	require.Equal(t, http.StatusOK, j.form(http.MethodPut, "/shots/"+j.shotID+"/locks", url.Values{"parameter": {"prompt"}, "mode": {"lock"}}).Code)

	rec := j.form(http.MethodPost, "/shots/"+j.shotID+"/generate/candidates", nil)

	assert.Equal(t, http.StatusLocked, rec.Code, rec.Body.String())
	assert.Equal(t, takes, j.count(`SELECT COUNT(*) FROM take`))
}

// The 生成 button of a Shot card posts to the Candidate flow wherever it can run (the Judge is on and
// the Prompt is not LOCKed); everywhere else it is the single generation (#437).
func TestCandidates_TheCardGenerateButtonPostsToTheCandidateFlowWhereItCanRun(t *testing.T) {
	j := newJudgeApp(t, goodVerdict, steadyFaces...)
	page := func() string {
		return j.get("/projects/"+j.project+"/scenes/"+j.sceneID+"?shot="+j.shotID, true).Body.String()
	}
	assert.Contains(t, page(), `hx-post="/shots/`+j.shotID+`/generate/candidates"`)
	assert.NotContains(t, page(), `hx-post="/shots/`+j.shotID+`/generate"`)

	j.generateAndJudge() // the Shot has a spec now
	j.setQuality("max_quality")
	assert.Contains(t, page(), `hx-post="/shots/`+j.shotID+`/generate/candidates"`)
	require.Equal(t, http.StatusOK, j.form(http.MethodPut, "/shots/"+j.shotID+"/locks", url.Values{"parameter": {"prompt"}, "mode": {"lock"}}).Code)
	html := page()
	assert.Contains(t, html, `hx-post="/shots/`+j.shotID+`/generate"`, "a LOCKed Prompt: the server would refuse the Candidates with 423")
	assert.NotContains(t, html, "/generate/candidates")

	off := newGenApp(t, true) // no Frames: the Judge is off
	html = off.get("/projects/"+off.project+"/scenes/"+off.sceneID+"?shot="+off.shotID, true).Body.String()
	assert.Contains(t, html, `hx-post="/shots/`+off.shotID+`/generate"`, "without the Judge the server answers /generate/candidates with 503")
	assert.NotContains(t, html, "/generate/candidates")
}

// The director's "regenerate" goes through the same entry as the card: under MAX QUALITY it makes a
// Candidate per strategy, not a single Take (#437).
func TestCandidates_DirectorRegenerateHonoursMaxQuality(t *testing.T) {
	const regenerate = `{"scene":1,"shot":1,"operations":[{"type":"regenerate","changes":["camera"],"preserve":[],"instruction":"camera movement: static"}]}`
	j := newJudgeAppOpts(t, judgeOpts{verdict: goodVerdict, command: regenerate}, steadyFaces...)
	j.director()
	j.generateAndJudge()
	j.setQuality("max_quality")
	before := j.count(`SELECT COUNT(*) FROM take`)

	rec := j.form(http.MethodPost, "/shots/"+j.shotID+"/director-command", url.Values{"content": {"カメラを固定して"}})
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())

	require.Eventually(t, func() bool {
		return j.count(`SELECT COUNT(*) FROM take WHERE json_extract(model_parameters, '$.candidate_strategy') IS NOT NULL`) == 3
	}, 30*time.Second, 10*time.Millisecond, "a Candidate per strategy")
	assert.Equal(t, before+3, j.count(`SELECT COUNT(*) FROM take`), "and no single Take besides")
}
