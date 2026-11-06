package router_test

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

func TestJudge_ALockedIdentityDriftFailsTheTakeWhateverTheModelSays(t *testing.T) {
	// The face drifts for frames 3-5 of 8 (1.5-3.0 s). The model gives a confident pass.
	// Two pictures of Mira let the thresholds come from their similarity (spec §12.4).
	j := newJudgeApp(t, goodVerdict, 0.95, 0.95, 0.95, 0.2, 0.2, 0.2, 0.95, 0.95)
	j.addMatchingCanonFace(t)
	ids := j.generateAndJudge()
	code, body := j.result(ids[0])
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "fail", body.Result)
	assert.InDelta(t, 0.93, body.OverallConfidence, 1e-9, "the model's confidence is reported as it is")
	require.Len(t, body.Issues, 1)
	is := body.Issues[0]
	assert.Equal(t, "face_identity_drift", is.Type)
	assert.Equal(t, "high", is.Severity)
	assert.Equal(t, "1.5-3.0s", is.TimeRange)
	assert.Equal(t, []int{38, 75}, is.Frames)
	assert.True(t, is.LockViolation, "the spec locks Mira's identity")
	require.NotNil(t, body.RepairStrategy)
	assert.Equal(t, "reference_reinforcement", body.RepairStrategy.Action)
	assert.Contains(t, body.RepairStrategy.Instructions, "increase_face_reference_strength")
	rows := j.get("/projects/"+j.project, false).Body.String()
	assert.Contains(t, rows, "LOCK 違反", "the chat says the Take failed on a LOCK violation")
}

func TestJudge_ALockSetOnTheShotMakesAMediumIssueInThatDimensionAViolation(t *testing.T) {
	// The model passes the Take but notes a medium Acting issue. Without a LOCK that is a pass with
	// a remark; with Acting LOCKed on the Shot (PUT /shots/:id/locks) it is a LOCK violation.
	const actingIssue = `{"result":"pass","overall_confidence":0.9,"issues":[{"type":"acting_drift","severity":"medium","dimension":"acting","time_range":"0.8-1.7s"}]}`
	judged := func(lock bool) (judgeBody, *judgeApp) {
		j := newJudgeApp(t, actingIssue, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95)
		if lock {
			require.NoError(t, repository.NewLockRepository(j.db).SetShotLock(context.Background(), j.shotID, domain.ParamActing, domain.LockLocked, nil))
		}
		ids := j.generateAndJudge()
		code, body := j.result(ids[0])
		require.Equal(t, http.StatusOK, code)
		return body, j
	}

	body, _ := judged(false)
	assert.Equal(t, "pass", body.Result, "without the LOCK the model's pass stands")
	require.Len(t, body.Issues, 1)
	assert.False(t, body.Issues[0].LockViolation)

	body, j := judged(true)
	assert.Equal(t, "fail", body.Result, "a LOCK on the Shot is a LOCK for the Judge")
	require.Len(t, body.Issues, 1)
	assert.True(t, body.Issues[0].LockViolation)
	assert.Contains(t, j.get("/projects/"+j.project, false).Body.String(), "LOCK 違反")
}

func TestJudge_PostRerunsTheJudgeAndKeepsEveryResultAsHistory(t *testing.T) {
	j := newJudgeApp(t, goodVerdict, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95)
	ids := j.generateAndJudge()
	take := ids[0]
	require.Equal(t, 1, j.results(t, take))

	// The next run is a fail: the latest result must be the new one.
	j.llm.mu.Lock()
	j.llm.verdict = `{"result":"fail","overall_confidence":0.8,"issues":[{"type":"camera_speed","severity":"medium","dimension":"camera_motion","time_range":"0.8-1.7s"}],"repair_strategy":{"action":"partial_regeneration","instructions":["reduce_camera_speed_40_percent"]}}`
	j.llm.mu.Unlock()
	rec := j.form(http.MethodPost, "/takes/"+take+"/judge", nil)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "<html", "a fragment")
	assert.Contains(t, rec.Body.String(), `id="shot-`+j.shotID+`"`)
	require.EventuallyWithT(t, func(c *assert.CollectT) { assert.Equal(c, 2, j.results(c, take)) }, 30*time.Second, 10*time.Millisecond)
	code, body := j.result(take)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "fail", body.Result, "the latest run")
	require.Len(t, body.Issues, 1)
	assert.Equal(t, "camera_speed", body.Issues[0].Type)
	assert.Equal(t, "0.8-1.7s", body.Issues[0].TimeRange)
	assert.Equal(t, []int{20, 43}, body.Issues[0].Frames)
	require.NotNil(t, body.RepairStrategy)
	assert.Equal(t, "partial_regeneration", body.RepairStrategy.Action)
	var passes int
	require.NoError(t, j.db.Reader().QueryRow(`SELECT COUNT(*) FROM judge_result WHERE take_id = ? AND result = 'pass'`, take).Scan(&passes))
	assert.Equal(t, 1, passes, "the first result is still there")
}

func TestJudge_UnknownTakeIs404AndAnUnjudgedTakeHasNoResult(t *testing.T) {
	j := newJudgeApp(t, goodVerdict, 0.95, 0.95, 0.95)
	assert.Equal(t, http.StatusNotFound, j.form(http.MethodPost, "/takes/nope/judge", nil).Code)
	code, _ := j.result("nope")
	assert.Equal(t, http.StatusNotFound, code)
	ids := j.generateAndJudge()
	require.NoError(t, j.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM judge_result WHERE take_id = ?`, ids[0])
		return err
	}))
	code, _ = j.result(ids[0])
	assert.Equal(t, http.StatusNotFound, code, "the Take exists but was never judged")
}

func TestJudge_AJudgeRunInFlightCannotBeStartedTwice(t *testing.T) {
	j := newJudgeApp(t, goodVerdict, 0.95, 0.95, 0.95)
	ids := j.generateAndJudge()
	// A judge job of the Take is on record: 'running', as the live job queue would claim a 'queued' row.
	require.NoError(t, j.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO job (id, kind, payload, status) VALUES ('stuck', 'judge', ?, 'running')`, `{"take_id":"`+ids[0]+`","shot_id":"`+j.shotID+`"}`)
		return err
	}))
	rec := j.form(http.MethodPost, "/takes/"+ids[0]+"/judge", nil)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.True(t, strings.Contains(rec.Body.String(), "Judge 中"), rec.Body.String())
}

// The Shot card draws a Judge button only for a Take the server accepts a Judge run of (#438): one that
// was never judged (or whose run failed) and is not being judged, on a Shot that is not busy.
func TestJudge_ButtonIsDrawnOnlyForUnjudgedTakesTheServerAccepts(t *testing.T) {
	j := newJudgeApp(t, goodVerdict, 0.95, 0.95, 0.95, 0.95)
	j.director()
	ids := j.generateAndJudge()
	page := func() string {
		return j.get("/projects/"+j.project+"/scenes/"+j.sceneID+"?shot="+j.shotID, true).Body.String()
	}
	button := func(html, take string) bool { return strings.Contains(html, "/takes/"+take+"/judge\"") }
	assert.False(t, button(page(), ids[0]), "judged: no button")
	assert.False(t, button(page(), ids[1]), "judged: no button")

	j.execSQL(`DELETE FROM judge_result WHERE take_id = ?`, ids[1])
	html := page()
	assert.True(t, button(html, ids[1]), "never judged")
	assert.False(t, button(html, ids[0]))

	// A judge run of the Take in flight ('running', not 'queued': see above): 409, so no button.
	j.execSQL(`INSERT INTO job (id, kind, payload, status) VALUES ('stuck', 'judge', ?, 'running')`, `{"take_id":"`+ids[1]+`","shot_id":"`+j.shotID+`"}`)
	assert.False(t, button(page(), ids[1]))
	assert.Equal(t, http.StatusConflict, j.form(http.MethodPost, "/takes/"+ids[1]+"/judge", nil).Code)
	j.execSQL(`DELETE FROM job WHERE id = 'stuck'`)

	// A Shot that is being generated: 409 too.
	j.execSQL(`UPDATE shot SET state = 'GENERATING' WHERE id = ?`, j.shotID)
	assert.False(t, button(page(), ids[1]))
	assert.Equal(t, http.StatusConflict, j.form(http.MethodPost, "/takes/"+ids[1]+"/judge", nil).Code)
	j.execSQL(`UPDATE shot SET state = 'PLANNING' WHERE id = ?`, j.shotID)

	// A Take without a video cannot be judged.
	var file string
	require.NoError(t, j.db.Reader().QueryRow(`SELECT file_path FROM take WHERE id = ?`, ids[1]).Scan(&file))
	j.execSQL(`UPDATE take SET file_path = NULL WHERE id = ?`, ids[1])
	assert.False(t, button(page(), ids[1]))
	assert.Equal(t, http.StatusConflict, j.form(http.MethodPost, "/takes/"+ids[1]+"/judge", nil).Code)
	j.execSQL(`UPDATE take SET file_path = ? WHERE id = ?`, file, ids[1])
	rec := j.form(http.MethodPost, "/takes/"+ids[1]+"/judge", nil)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	require.EventuallyWithT(t, func(c *assert.CollectT) { assert.Equal(c, 1, j.results(c, ids[1])) }, 30*time.Second, 10*time.Millisecond)
	assert.False(t, button(page(), ids[1]), "judged again")
}

// Without the Judge POST /takes/:id/judge is always 503, so no card offers it.
func TestJudge_NoButtonWithoutTheJudge(t *testing.T) {
	g := newGenApp(t, true)
	g.generateTakes()
	html := g.get("/projects/"+g.project+"/scenes/"+g.sceneID+"?shot="+g.shotID, true).Body.String()
	assert.Contains(t, html, "/takes/", "the Takes are listed")
	assert.NotContains(t, html, "/judge\"")
}

func TestJudge_IsUnavailableWithoutFrameSampling(t *testing.T) {
	g := newGenApp(t, true) // the generation app has no Frames: the Judge is off
	assert.Equal(t, http.StatusServiceUnavailable, g.form(http.MethodPost, "/takes/anything/judge", nil).Code)
	g.generateTakes()
	var id string
	require.NoError(t, g.db.Reader().QueryRow(`SELECT id FROM take LIMIT 1`).Scan(&id))
	assert.Equal(t, http.StatusServiceUnavailable, g.form(http.MethodPost, "/takes/"+id+"/judge", nil).Code)
	assert.Equal(t, http.StatusNotFound, g.get("/api/v1/takes/"+id+"/judge-result", false).Code)
}

func TestJudge_TheShotInspectorShowsEachTakesLatestJudgeResultAndTheGenerationModel(t *testing.T) {
	const actingIssue = `{"result":"pass","overall_confidence":0.9,"issues":[{"type":"acting_drift","severity":"medium","dimension":"acting","time_range":"0.8-1.7s","description":"表情が急に変わる"}]}`
	j := newJudgeApp(t, actingIssue, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95)
	require.NoError(t, repository.NewLockRepository(j.db).SetShotLock(context.Background(), j.shotID, domain.ParamActing, domain.LockLocked, nil))
	before := j.get("/shots/"+j.shotID, false).Body.String()
	assert.NotContains(t, before, "data-judge-result")
	j.generateAndJudge()

	html := j.get("/shots/"+j.shotID, false).Body.String()
	assert.Contains(t, html, "data-judge-result")
	assert.Contains(t, html, "Judge: FAIL（確信度 90%）")
	assert.Contains(t, html, "LOCK 違反 / medium / acting_drift / 0.8-1.7s: 表情が急に変わる")
	assert.Contains(t, html, `aria-label="Generation Model"`)
	assert.NotContains(t, html, "自動選択（生成時に決定）", "the Shot has a Take, whose model is the Generation Model")
}
