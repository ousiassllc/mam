package router_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/jev"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
)

// A Take the Judge fails on a plain (not LOCKed) camera issue; the Judge recommends Partial Regeneration.
const failVerdict = `{"result":"fail","overall_confidence":0.8,"issues":[{"type":"camera_speed","severity":"medium","dimension":"camera_motion"}],
	"preserve":["character"],"repair_strategy":{"action":"partial_regeneration","instructions":["reduce_camera_speed_40_percent"]}}`

// director switches the Project to DIRECTOR MODE: nothing is repaired unless the user asks.
func (j *judgeApp) director() {
	j.execSQL(`UPDATE project SET mode = 'director' WHERE id = ?`, j.project)
}

func TestRepair_PostMakesANewTakeThatIsTracedAndLeavesTheRepairedTakeAsItIs(t *testing.T) {
	j := newJudgeApp(t, failVerdict, 0.95, 0.95, 0.95, 0.95)
	j.director()
	ids := j.generateAndJudge()
	source := ids[0]
	j.video.gate = make(chan struct{}) // the repair's video call waits here, so the answer is read while REPAIRING
	rec := j.form(http.MethodPost, "/takes/"+source+"/repair", nil)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `id="shot-`+j.shotID+`"`, "the Shot card")
	assert.Contains(t, rec.Body.String(), "Repair 中", "REPAIRING")
	close(j.video.gate)
	j.waitTakes(4)
	require.Eventually(t, func() bool {
		return j.count(`SELECT COUNT(*) FROM repair_action WHERE resulting_take_id IS NOT NULL`) == 1
	}, 30*time.Second, 10*time.Millisecond)
	var action, resulting, sourceInRow, decidedBy string
	require.NoError(t, j.db.Reader().QueryRow(`SELECT action_type, resulting_take_id, json_extract(instructions, '$.source_take_id'), json_extract(instructions, '$.decided_by') FROM repair_action`).
		Scan(&action, &resulting, &sourceInRow, &decidedBy))
	assert.Equal(t, "partial_regeneration", action, "the Judge's recommendation is used when the Repair Agent gives no valid choice")
	assert.Equal(t, "rule", decidedBy)
	assert.Equal(t, source, sourceInRow)
	assert.NotContains(t, ids, resulting, "a new take row")
	assert.Equal(t, 1, j.count(`SELECT COUNT(*) FROM take WHERE id = ?`, source), "the repaired Take is still there")

	// The new Take is judged, and DIRECTOR MODE does not go on by itself.
	require.Eventually(t, func() bool { return j.results(resulting) == 1 }, 30*time.Second, 10*time.Millisecond)
	assert.Eventually(t, func() bool {
		page := j.get("/projects/"+j.project+"/scenes/"+j.sceneID+"?shot="+j.shotID, true).Body.String()
		return !strings.Contains(page, "Repair 中") && !strings.Contains(page, "Judge 中")
	}, 30*time.Second, 10*time.Millisecond)
	assert.Equal(t, 4, j.count(`SELECT COUNT(*) FROM take`))
}

func TestRepair_UnknownJudgedAndAcceptedTakesAreRefused(t *testing.T) {
	j := newJudgeApp(t, goodVerdict, 0.95, 0.95, 0.95)
	j.director()
	assert.Equal(t, http.StatusNotFound, j.form(http.MethodPost, "/takes/nope/repair", nil).Code)

	ids := j.generateAndJudge()
	assert.Equal(t, http.StatusConflict, j.form(http.MethodPost, "/takes/"+ids[0]+"/repair", nil).Code, "a Take that passed needs no repair")

	j.execSQL(`DELETE FROM judge_result WHERE take_id = ?`, ids[1])
	assert.Equal(t, http.StatusConflict, j.form(http.MethodPost, "/takes/"+ids[1]+"/repair", nil).Code, "a Take that was not judged")
	assert.Equal(t, 3, j.count(`SELECT COUNT(*) FROM take`))
	assert.Zero(t, j.count(`SELECT COUNT(*) FROM repair_action`))
}

// The Shot card draws a Repair button only for a Take the server accepts a Repair of (#336): a Take
// that passed the Judge or was never judged is refused with 409, so it gets no button.
func TestRepair_ButtonIsDrawnOnlyForTakesTheServerAccepts(t *testing.T) {
	page := func(j *judgeApp) string {
		return j.get("/projects/"+j.project+"/scenes/"+j.sceneID+"?shot="+j.shotID, true).Body.String()
	}

	passed := newJudgeApp(t, goodVerdict, 0.95, 0.95, 0.95)
	passed.director()
	ids := passed.generateAndJudge()
	assert.NotContains(t, page(passed), "/repair\"", "every Take passed: nothing to repair")
	assert.Equal(t, http.StatusConflict, passed.form(http.MethodPost, "/takes/"+ids[0]+"/repair", nil).Code)

	failed := newJudgeApp(t, failVerdict, 0.95, 0.95, 0.95, 0.95)
	failed.director()
	ids = failed.generateAndJudge()
	failed.execSQL(`DELETE FROM judge_result WHERE take_id = ?`, ids[1])
	html := page(failed)
	assert.Contains(t, html, "/takes/"+ids[0]+"/repair\"", "judged and failed")
	assert.NotContains(t, html, "/takes/"+ids[1]+"/repair\"", "never judged: 409")
	assert.Equal(t, http.StatusConflict, failed.form(http.MethodPost, "/takes/"+ids[1]+"/repair", nil).Code)
}

func TestRepair_ALowScoreFailsAPassAndStartsARepair(t *testing.T) {
	// The Judge says pass, but the drama Take scores 50 on every axis: under the Minimum acceptance score.
	scored := `{"result":"pass","overall_confidence":0.9,"issues":[],"scores":{"story":50,"acting":50,"character_identity":50,"continuity":50,"dialogue":50,"emotion":50,"camera":50,"editing_rhythm":50}}`
	j := newJudgeApp(t, scored, 0.95, 0.95, 0.95)
	j.director()
	j.execSQL(`UPDATE project SET production_template = 'drama' WHERE id = ?`, j.project)
	ids := j.generateAndJudge()

	rec := j.form(http.MethodPost, "/takes/"+ids[0]+"/repair", nil)
	assert.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	j.waitTakes(4)
}

func TestRepair_AutoModeRepairsUntilMaxRetriesThenEscalates(t *testing.T) {
	j := newJudgeApp(t, failVerdict, 0.95, 0.95, 0.95, 0.95)
	j.execSQL(`INSERT INTO project_policy (project_id, max_retries) VALUES (?, 2)`, j.project)

	j.generateTakes()
	require.Eventually(t, func() bool {
		return j.count(`SELECT COUNT(*) FROM chat_message WHERE kind = 'escalation'`) == 1
	}, 60*time.Second, 20*time.Millisecond, "at the limit the user is asked")

	assert.Equal(t, 5, j.count(`SELECT COUNT(*) FROM take`), "3 takes and 2 retries")
	assert.Equal(t, 2, j.count(`SELECT COUNT(*) FROM repair_action WHERE resulting_take_id IS NOT NULL`))
	assert.Equal(t, 2, j.count(`SELECT COUNT(*) FROM repair_action`), "the escalation is no repair")
	require.Eventually(t, func() bool { return j.count(`SELECT COUNT(*) FROM job WHERE status IN ('queued', 'running')`) == 0 }, 30*time.Second, 10*time.Millisecond, "every job ends")
	assert.Equal(t, 1, j.count(`SELECT COUNT(*) FROM chat_message WHERE kind = 'escalation'`), "asked once")
	assert.Equal(t, 5, j.count(`SELECT COUNT(*) FROM take`), "and nothing is generated afterwards")
}

func TestRepair_TheShotBudgetThatCannotPayTheNextRetryEscalatesInsteadOfGenerating(t *testing.T) {
	j := newJudgeApp(t, failVerdict, 0.95, 0.95, 0.95, 0.95)
	j.director()
	ids := j.generateAndJudge()
	j.execSQL(`UPDATE project SET shot_budget_ceiling = 0.0001 WHERE id = ?`, j.project) // Max cost per Shot: already spent

	require.Equal(t, http.StatusAccepted, j.form(http.MethodPost, "/takes/"+ids[0]+"/repair", nil).Code)
	require.Eventually(t, func() bool {
		return j.count(`SELECT COUNT(*) FROM chat_message WHERE kind = 'escalation'`) == 1
	}, 30*time.Second, 10*time.Millisecond)
	assert.Zero(t, j.count(`SELECT COUNT(*) FROM repair_action`), "no retry was paid for")
	assert.Equal(t, 3, j.count(`SELECT COUNT(*) FROM take`))
}

func TestRepair_DifferentModelSwitchesModelAndJevRunsInShadowOnly(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"model":"jev-1.13.0","answers":{"repair_strategy":{"type":"choice","choice":"full_retake","probabilities":{"full_retake":0.9,"different_model":0.1},"confidence":0.9}},"usage":{"input_tokens":400,"output_tokens":3}}`))
	}))
	defer srv.Close()
	cfg := jev.DefaultConfig()
	cfg.BaseURL = srv.URL
	keys := credentialtest.New()
	client, err := jev.New(cfg, keys, srv.Client())
	require.NoError(t, err)

	j := newJudgeAppOpts(t, judgeOpts{verdict: failVerdict, shadow: client, keys: keys}, 0.95, 0.95, 0.95, 0.95)
	require.NoError(t, keys.Set(jev.CredentialProvider, "typesafe-key"))
	j.director()
	j.llm.repair = `{"action":"different_model","reason":"the model keeps failing","instructions":["reduce_camera_speed"]}`
	ids := j.generateAndJudge()

	require.Equal(t, http.StatusAccepted, j.form(http.MethodPost, "/takes/"+ids[0]+"/repair", nil).Code)
	j.waitTakes(4)
	require.Eventually(t, func() bool {
		return j.count(`SELECT COUNT(*) FROM repair_shadow`) == 1 && j.count(`SELECT COUNT(*) FROM repair_action WHERE resulting_take_id IS NOT NULL`) == 1
	}, 30*time.Second, 10*time.Millisecond)

	var sourceModel, newModel, action, decidedBy string
	require.NoError(t, j.db.Reader().QueryRow(`SELECT model_name FROM take WHERE id = ?`, ids[0]).Scan(&sourceModel))
	require.NoError(t, j.db.Reader().QueryRow(`SELECT t.model_name, a.action_type, json_extract(a.instructions, '$.decided_by') FROM repair_action a JOIN take t ON t.id = a.resulting_take_id`).
		Scan(&newModel, &action, &decidedBy))
	assert.Equal(t, "different_model", action, "Jev's answer (full_retake) does not change the repair")
	assert.Equal(t, "llm", decidedBy)
	assert.NotEqual(t, sourceModel, newModel, "the other model of the Quality Policy: Kling 3.0 <-> Seedance 2.0")

	var jevChoice, decided, model string
	var agreed int
	var confidence float64
	require.NoError(t, j.db.Reader().QueryRow(`SELECT jev_choice, decided_action, agreed, confidence, model FROM repair_shadow`).Scan(&jevChoice, &decided, &agreed, &confidence, &model))
	assert.Equal(t, "full_retake", jevChoice)
	assert.Equal(t, "different_model", decided)
	assert.Zero(t, agreed, "Jev disagreed with the Repair Agent")
	assert.InDelta(t, 0.9, confidence, 1e-9)
	assert.Equal(t, "jev-1.13.0", model)
	assert.Equal(t, 1, calls)

	rec := j.get("/api/v1/repair/jev-shadow-report", false)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var report struct {
		Runs          int     `json:"runs"`
		Agreed        int     `json:"agreed"`
		AgreementRate float64 `json:"agreement_rate"`
		Confidence    []int   `json:"confidence_histogram"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &report))
	assert.Equal(t, 1, report.Runs)
	assert.Zero(t, report.Agreed)
	assert.Zero(t, report.AgreementRate)
	require.Len(t, report.Confidence, 10)
	assert.Equal(t, 1, report.Confidence[9], "confidence 0.9 falls in the last bucket")
}

func TestRepair_JevIsNotAskedWhenARuleOrTheLockDecides(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("Jev must not be called") }))
	defer srv.Close()
	cfg := jev.DefaultConfig()
	cfg.BaseURL = srv.URL
	keys := credentialtest.New()
	client, err := jev.New(cfg, keys, srv.Client())
	require.NoError(t, err)

	j := newJudgeAppOpts(t, judgeOpts{verdict: failVerdict, shadow: client, keys: keys}, 0.95, 0.95, 0.95, 0.95)
	require.NoError(t, keys.Set(jev.CredentialProvider, "typesafe-key"))
	j.director()
	j.llm.repair = "" // no valid answer: the fallback (a rule) decides
	ids := j.generateAndJudge()

	require.Equal(t, http.StatusAccepted, j.form(http.MethodPost, "/takes/"+ids[0]+"/repair", nil).Code)
	j.waitTakes(4)
	require.Eventually(t, func() bool {
		return j.count(`SELECT COUNT(*) FROM repair_action WHERE resulting_take_id IS NOT NULL`) == 1
	}, 30*time.Second, 10*time.Millisecond)
	assert.Zero(t, j.count(`SELECT COUNT(*) FROM repair_shadow`))
}
