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

func TestCandidates_BalancedModeStopsAtTheFirstCandidateThatPasses(t *testing.T) {
	j := newJudgeApp(t, goodVerdict, steadyFaces...)
	j.candidates()

	assert.Equal(t, []string{"stability_first"}, j.candidateStrategies())
	assert.Equal(t, 1, j.llm.judgeCalls())
}

func TestCandidates_BalancedModeAddsTheNextCandidateWhileTheJudgeFailsAndStopsAtTheRetryLimit(t *testing.T) {
	j := newJudgeApp(t, goodVerdict, driftFaces...)
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
