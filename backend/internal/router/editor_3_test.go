package router_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// The additional Shot request that stops at the limit of the Retry Policy raises an Escalation card and
// moves the Project to AWAITING_APPROVAL, where the timeline editor is readonly: the response carries
// timeline-reload so that the editor on the page does not stay editable (#335).
func TestEditor_RequestAdditionalShotAtTheRetryLimitEscalatesAndReloadsTheTimeline(t *testing.T) {
	// The generation is available (it is checked before the Retry Policy); no Shot is ever generated here.
	e := newEditorAppWith(t, func(d *service.Deps) {
		d.Jobs = jobqueue.New(repository.NewJobRepository(d.DB), jobqueue.Options{})
		d.Models = modeladapter.New(modeladapter.DefaultConfig(), modeladapter.Backends{})
	})
	e.setState("VISUAL_DESIGN")
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/takes/t1/select", nil).Code)
	require.Equal(t, http.StatusOK, e.roughCut().Code)
	id := e.timeline().ID
	e.llm.answer = `{"verdict":"need_shot","findings":[{"clip_index":1,"issue":"リアクションがない","action":"足す",` +
		`"need_shot":{"kind":"reaction","after_clip":1,"objective":"驚く","duration_seconds":2}}]}`
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/edit-timelines/"+id+"/judge", nil).Code)
	e.execSQL(`INSERT INTO project_policy (project_id, max_retries) VALUES (?, 0) ON CONFLICT (project_id) DO UPDATE SET max_retries = 0`, e.project)
	rec := e.form(http.MethodPost, "/edit-timelines/"+id+"/request-additional-shot", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "AWAITING_APPROVAL", e.state())
	assert.Equal(t, "timeline-reload", rec.Header().Get("HX-Trigger"))

	var body struct {
		Readonly bool `json:"readonly"`
	}
	require.NoError(t, json.Unmarshal(e.get("/api/v1/projects/"+e.project+"/edit-timeline", false).Body.Bytes(), &body))
	assert.True(t, body.Readonly, "the reload the event causes gets a readonly timeline")
}

func TestEditor_NeedShotVerdictIsReportedWithTheShotToMakeAndAwaitsTheDirector(t *testing.T) {
	e := newEditorApp(t)
	e.setState("VISUAL_DESIGN")
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/takes/t1/select", nil).Code)
	require.Equal(t, http.StatusOK, e.roughCut().Code)
	id := e.timeline().ID

	// Not judged yet: nothing to act on.
	assert.Equal(t, http.StatusConflict, e.form(http.MethodPost, "/edit-timelines/"+id+"/request-additional-shot", nil).Code)
	e.llm.answer = `{"verdict":"need_shot","findings":[{"clip_index":1,"issue":"リアクションがない","action":"足す",` +
		`"need_shot":{"kind":"reaction","after_clip":1,"objective":"驚く","duration_seconds":2}}]}`
	rec := e.form(http.MethodPost, "/edit-timelines/"+id+"/judge", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "EDIT_JUDGING", e.state(), "the Project waits for the additional Shot")
	assert.Contains(t, rec.Body.String(), "data-need-shot")
	j := e.judge()
	require.NotNil(t, j)
	assert.Equal(t, domain.EditorNeedShot, j.Verdict)
	require.Len(t, j.Findings, 1)
	require.NotNil(t, j.Findings[0].Need)
	assert.Equal(t, domain.ClipReaction, j.Findings[0].Need.Kind)
	assert.Equal(t, 1, j.Findings[0].Need.AfterClip)
	assert.NotContains(t, e.get("/api/v1/projects/"+e.project+"/edit-timeline", false).Body.String(), "judge", "the Judge result is Templ's (editorpanel.Actions), not an API field")
	assert.NotContains(t, e.get("/api/v1/projects/"+e.project+"/edit-timeline", false).Body.String(), "request_shot_url", "the button is Templ's (hx-post), not an API field")

	// Without generation configured (no video model) the Director cannot be asked.
	assert.Equal(t, http.StatusServiceUnavailable, e.form(http.MethodPost, "/edit-timelines/"+id+"/request-additional-shot", nil).Code)
}

func TestEditor_RoughCutKeepsTheClipOfAShotWhoseEditingIsFixed(t *testing.T) {
	for _, mode := range []string{"manual", "lock"} {
		t.Run(mode, func(t *testing.T) {
			e := newEditorApp(t)
			e.setState("VISUAL_DESIGN")
			require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/takes/t1/select", nil).Code)
			require.Equal(t, http.StatusOK, e.roughCut().Code)
			tl := e.timeline()
			body := `{"clips":[{"shot_id":"` + e.shotID + `","take_id":"t1","in_point":1,"out_point":3,"transition":{"type":"fade","seconds":0.5}}]}`
			require.Equal(t, http.StatusNoContent, e.request(http.MethodPut, "/edit-timelines/"+tl.ID, []byte(body), "application/json", false).Code)
			require.Equal(t, http.StatusOK, e.form(http.MethodPut, "/shots/"+e.shotID+"/locks", url.Values{"parameter": {"editing"}, "mode": {mode}}).Code)
			require.Equal(t, http.StatusOK, e.roughCut().Code)
			got := e.timeline()
			require.Len(t, got.Clips, 1)
			assert.InDelta(t, 1, got.Clips[0].In, 1e-9, "Editing %s: the user's cut survives a new Rough Cut", mode)
			assert.InDelta(t, 2, got.Duration, 0.01)
		})
	}

	t.Run("auto", func(t *testing.T) {
		e := newEditorApp(t)
		e.setState("VISUAL_DESIGN")
		require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/takes/t1/select", nil).Code)
		require.Equal(t, http.StatusOK, e.roughCut().Code)
		tl := e.timeline()
		body := `{"clips":[{"shot_id":"` + e.shotID + `","take_id":"t1","in_point":1,"out_point":3,"transition":{"type":"fade","seconds":0.5}}]}`
		require.Equal(t, http.StatusNoContent, e.request(http.MethodPut, "/edit-timelines/"+tl.ID, []byte(body), "application/json", false).Code)
		require.Equal(t, http.StatusOK, e.roughCut().Code)
		assert.InDelta(t, 4, e.timeline().Duration, 0.01, "AUTO: the AI cuts the Shot afresh")
	})
}

func TestEditor_AudioKeepsTheEarlierBGMWhenMusicIsFixed(t *testing.T) {
	e := newEditorApp(t)
	e.setState("VISUAL_DESIGN")
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/takes/t1/select", nil).Code)
	require.Equal(t, http.StatusOK, e.roughCut().Code)
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/edit-timelines/"+e.timeline().ID+"/judge", nil).Code)
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/projects/"+e.project+"/audio/generate", nil).Code)
	repo := repository.NewEditorRepository(e.db)
	first, err := repo.LatestAudioTimeline(context.Background(), e.project, "")
	require.NoError(t, err)
	var bgm domain.AudioClip
	for _, c := range first.Timeline.Clips {
		if c.LayerOf() == domain.LayerBGM {
			bgm = c
		}
	}
	require.NotEmpty(t, bgm.Path)
	require.Equal(t, http.StatusOK, e.form(http.MethodPut, "/projects/"+e.project+"/locks", url.Values{"parameter": {"music"}, "mode": {"lock"}}).Code)
	musicCalls := func() int {
		var n int
		require.NoError(t, e.db.Reader().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM cost_ledger_entry WHERE project_id = ? AND provider = 'elevenlabs-music'`, e.project).Scan(&n))
		return n
	}
	before := musicCalls()
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/projects/"+e.project+"/audio/generate", nil).Code)
	second, err := repo.LatestAudioTimeline(context.Background(), e.project, "")
	require.NoError(t, err)
	assert.Equal(t, first.VersionNumber+1, second.VersionNumber)
	assert.Equal(t, before, musicCalls(), "Music LOCK: no new music is paid for")
	var kept []domain.AudioClip
	for _, c := range second.Timeline.Clips {
		if c.LayerOf() == domain.LayerBGM {
			kept = append(kept, c)
		}
	}
	assert.Equal(t, []domain.AudioClip{bgm}, kept, "the earlier BGM is carried over")
}

func TestEditor_AutoChainRunsFromTheLastSelectedTakeToReady(t *testing.T) {
	e := newEditorApp(t)
	e.setMode("auto")
	e.setState("JUDGING")
	doc := domain.ScriptDoc{Script: domain.Script{Scenes: []domain.ScriptScene{{Title: "Street", Dialogue: []string{"ナレーション: 朝が来た"}}}}}
	_, err := repository.NewPlanRepository(e.db).SaveScript(context.Background(), e.project, doc)
	require.NoError(t, err)

	// The only Shot gets its Selected Take: Rough Cut, Editor Judge, audio and Final QC follow on their own.
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/takes/t1/select", nil).Code)
	require.EventuallyWithT(t, func(c *assert.CollectT) { assert.Equal(c, "READY", e.stateOn(c)) }, 60*time.Second, 50*time.Millisecond, "state: %s", e.state())
	assert.Equal(t, domain.EditorPass, e.judge().Verdict)
	audio, err := repository.NewEditorRepository(e.db).LatestAudioTimeline(context.Background(), e.project, "")
	require.NoError(t, err)
	assert.Equal(t, 1, audio.VersionNumber)
	qc, err := repository.NewEditorRepository(e.db).LatestFinalQC(context.Background(), e.project)
	require.NoError(t, err)
	assert.Equal(t, audio.ID, qc.AudioTimelineVersionID)
	exports, err := repository.NewEditorRepository(e.db).ListExports(context.Background(), e.project)
	require.NoError(t, err)
	assert.Empty(t, exports, "the export is the user's action")
}

func TestEditor_AutoChainStopsAtAReEditVerdict(t *testing.T) {
	e := newEditorApp(t)
	e.setMode("auto")
	e.setState("JUDGING")
	e.llm.answer = `{"verdict":"re_edit","findings":[{"clip_index":1,"issue":"冒頭が長い","action":"1 秒短くする"}]}`
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/takes/t1/select", nil).Code)
	require.EventuallyWithT(t, func(c *assert.CollectT) { assert.NotNil(c, e.judgeOn(c)) }, 30*time.Second, 50*time.Millisecond)
	assert.Equal(t, domain.EditorReEdit, e.judge().Verdict)
	time.Sleep(300 * time.Millisecond)
	assert.Equal(t, "EDITING", e.state(), "a Re-edit verdict waits for the user")
	_, err := repository.NewEditorRepository(e.db).LatestAudioTimeline(context.Background(), e.project, "")
	assert.ErrorIs(t, err, repository.ErrNotFound)
}

func TestEditor_JudgeStopsOnADatabaseErrorInsteadOfReportingAMissingTakeFile(t *testing.T) {
	e := newEditorApp(t)
	e.setState("VISUAL_DESIGN")
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/takes/t1/select", nil).Code)
	require.Equal(t, http.StatusOK, e.roughCut().Code)
	id := e.timeline().ID
	// A Take row that cannot be read back (its scores are not a Take Scores object).
	e.execSQL(`UPDATE take SET scores = '"broken"' WHERE id = 't1'`)

	rec := e.form(http.MethodPost, "/edit-timelines/"+id+"/judge", nil)
	assert.GreaterOrEqual(t, rec.Code, http.StatusInternalServerError, rec.Body.String())
	assert.Equal(t, "EDITING", e.state())
	_, err := repository.NewEditorRepository(e.db).LatestEditorJudge(context.Background(), id)
	assert.ErrorIs(t, err, repository.ErrNotFound, "no false Re-edit finding is stored")
}

func TestEditor_AudioFilesAreRemovedWhenTheTimelineCannotBeSaved(t *testing.T) {
	e := newEditorApp(t)
	e.setState("VISUAL_DESIGN")
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/takes/t1/select", nil).Code)
	require.Equal(t, http.StatusOK, e.roughCut().Code)
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/edit-timelines/"+e.timeline().ID+"/judge", nil).Code)
	e.execSQL(`CREATE TRIGGER refuse_audio BEFORE INSERT ON audio_timeline_version BEGIN SELECT RAISE(ABORT, 'disk full'); END`)
	rec := e.form(http.MethodPost, "/projects/"+e.project+"/audio/generate", nil)
	assert.NotEqual(t, http.StatusOK, rec.Code)
	var paid int
	require.NoError(t, e.db.Reader().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM cost_ledger_entry WHERE project_id = ? AND provider LIKE 'elevenlabs-%'`, e.project).Scan(&paid))
	assert.Positive(t, paid, "the audio was made and paid for before the save failed; the ledger keeps it")
	runs, err := filepath.Glob(filepath.Join(e.dir, "assets", e.project, "audio", e.timeline().ID, "*"))
	require.NoError(t, err)
	assert.Empty(t, runs, "no run directory is left")
}

func TestEditor_ABranchRoughCutFadesAtTheSceneBoundaryAndNumbersTheScenesOfTheStory(t *testing.T) {
	e := newEditorApp(t)
	e.setState("VISUAL_DESIGN")
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/takes/t1/select", nil).Code)
	rec := e.form(http.MethodPost, "/shots/"+e.shotID+"/branch", url.Values{"name": {"逃げる"}})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	branchID := strings.TrimPrefix(rec.Header().Get("HX-Redirect"), "/branches/")

	// The Branch's own first Scene counts from 0 again, like the trunk's first Scene does.
	ctx := context.Background()
	plans := repository.NewPlanRepository(e.db)
	scene, err := plans.CreateScene(ctx, domain.Scene{ProjectID: e.project, BranchID: branchID, Title: "Door"})
	require.NoError(t, err)
	require.Equal(t, 0, scene.OrderIndex)
	shot, err := plans.CreateShot(ctx, domain.Shot{SceneID: scene.ID, Objective: "扉を開ける", Duration: 2})
	require.NoError(t, err)
	e.video("b1", "1920x1080", 4)
	e.execSQL(`INSERT INTO prompt_version (id, shot_id, version_number, shot_spec_snapshot) VALUES ('pvb', ?, 1, '{}')`, shot.ID)
	e.execSQL(`INSERT INTO asset (id, project_id, type, file_path, mime_type) VALUES ('a-b1', ?, 'video', ?, 'video/mp4')`, e.project, e.project+"/b1.mp4")
	e.execSQL(`INSERT INTO take (id, prompt_version_id, model_name, model_parameters, file_path, created_at) VALUES ('b1', 'pvb', 'kling-3.0', '{"resolution":"1080p","duration_seconds":4}', ?, '2026-01-01T00:00:09.000Z')`, e.project+"/b1.mp4")
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/takes/b1/select", nil).Code)
	rec = e.form(http.MethodPost, "/projects/"+e.project+"/edit/rough-cut", url.Values{"branch_id": {branchID}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	tl, code := e.timelineOn(branchID)
	require.Equal(t, http.StatusOK, code)
	require.Len(t, tl.Clips, 2)
	assert.Equal(t, e.shotID, tl.Clips[0].ShotID, "the inherited Shot comes first")
	assert.Equal(t, shot.ID, tl.Clips[1].ShotID)
	assert.Equal(t, "fade", tl.Clips[1].Transition.Type, "the Branch's first Scene is a Scene boundary")
	assert.InDelta(t, domain.DefaultTransitionSeconds, tl.Clips[1].Transition.Seconds, 1e-9)
	assert.NotEqual(t, tl.Clips[0].Label, tl.Clips[1].Label, "the two Scenes are told apart")
	assert.Equal(t, "Scene 2 · Shot 1", tl.Clips[1].Label)
}
