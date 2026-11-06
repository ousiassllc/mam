package router_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

func TestEditor_FullFlowFromRoughCutToExport(t *testing.T) {
	e := newEditorApp(t)
	e.setState("VISUAL_DESIGN")
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/takes/t1/select", nil).Code)
	doc := domain.ScriptDoc{Script: domain.Script{Scenes: []domain.ScriptScene{{Title: "Street", Dialogue: []string{"ナレーション: 朝が来た", "ユイ: 行こう"}}}}}
	_, err := repository.NewPlanRepository(e.db).SaveScript(context.Background(), e.project, doc)
	require.NoError(t, err)

	// Rough Cut: the Selected Take from the start for the planned 4 s, EDITING.
	rec := e.roughCut()
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "timeline-reload", rec.Header().Get("HX-Trigger"))
	assert.Equal(t, "EDITING", e.state())
	// The stage moved the Project: the answer refreshes the state badge of the page header too.
	assert.Regexp(t, `<span[^>]*id="project-state"[^>]*hx-swap-oob="true"|<span[^>]*hx-swap-oob="true"[^>]*id="project-state"`, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `data-state="EDITING"`)
	tl := e.timeline()
	require.Len(t, tl.Clips, 1)
	assert.Equal(t, "t1", tl.Clips[0].TakeID)
	assert.InDelta(t, 4, tl.Duration, 0.01)
	assert.False(t, tl.Readonly)

	// Editing: shorten the clip to 2 s with a fade; the save is a new version.
	body := `{"clips":[{"shot_id":"` + e.shotID + `","take_id":"t1","in_point":1,"out_point":3,"transition":{"type":"fade","seconds":0.5}}]}`
	rec = e.request(http.MethodPut, "/edit-timelines/"+tl.ID, []byte(body), "application/json", false)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	saved := e.timeline()
	assert.Equal(t, tl.Version+1, saved.Version)
	assert.InDelta(t, 2, saved.Duration, 0.01)
	assert.InDelta(t, 1, saved.Clips[0].In, 1e-9)

	// The old version cannot be saved over the new one.
	rec = e.request(http.MethodPut, "/edit-timelines/"+tl.ID, []byte(body), "application/json", false)
	assert.Equal(t, http.StatusConflict, rec.Code)

	// Preview of the rendered Rough Cut.
	rec = e.get("/edit-timelines/"+saved.ID+"/preview", false)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "video/mp4", rec.Header().Get("Content-Type"))
	assert.Equal(t, "sandbox", rec.Header().Get("Content-Security-Policy"))

	// Editor Judge: PASS moves the Project to AUDIO (the default level asks for no Rough Cut card).
	rec = e.form(http.MethodPost, "/edit-timelines/"+saved.ID+"/judge", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "timeline-reload", rec.Header().Get("HX-Trigger"), "the timeline editor reloads once the Project moved on")
	assert.Equal(t, "AUDIO", e.state())
	assert.Equal(t, domain.EditorPass, e.judge().Verdict)

	// Audio: two lines are spoken, the Scene gets the BGM and SE its Emotion asks for, all mixed and normalized.
	rec = e.form(http.MethodPost, "/projects/"+e.project+"/audio/generate", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "Dialogue 1")
	assert.Contains(t, rec.Body.String(), "BGM 1")
	latest, err := repository.NewEditorRepository(e.db).LatestAudioTimeline(context.Background(), e.project, "")
	require.NoError(t, err)
	audio := latest.Timeline
	assert.Equal(t, domain.AudioModeDirector, audio.Mode)
	assert.Equal(t, 1, latest.VersionNumber)
	assert.Equal(t, 2, audio.SpeechLines())
	assert.Equal(t, 1, audio.Count(domain.LayerBGM))
	assert.Equal(t, 1, audio.Count(domain.LayerSE))
	require.Len(t, audio.Scenes, 1)
	assert.Equal(t, "期待", audio.Scenes[0].Emotion)
	assert.InDelta(t, domain.DefaultTargetLUFS, audio.MeasuredLUFS, 1.5)
	// Every provider call is in the cost ledger under the identifier of model-adapters.md §6.
	var providers string
	require.NoError(t, e.db.Reader().QueryRow(`SELECT group_concat(provider) FROM (SELECT provider FROM cost_ledger_entry WHERE project_id = ? AND provider LIKE 'elevenlabs-%' ORDER BY provider)`, e.project).Scan(&providers))
	assert.Equal(t, "elevenlabs-music,elevenlabs-sfx,elevenlabs-tts,elevenlabs-tts", providers)
	_, statErr := os.Stat(filepath.Join(e.dir, "assets", filepath.FromSlash(audio.MixPath)))
	assert.NoError(t, statErr)

	// Final QC: PASS makes the Project READY.
	rec = e.form(http.MethodPost, "/projects/"+e.project+"/final-qc", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "READY", e.state())

	// Export: 9:16 at 720p in VP9 (WebM), EXPORTED, atomic (no temp file left behind).
	rec = e.form(http.MethodPost, "/projects/"+e.project+"/export", url.Values{"aspect_ratio": {"9:16"}, "resolution": {"720p"}, "codec": {"vp9"}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "EXPORTED", e.state())
	exports, err := repository.NewEditorRepository(e.db).ListExports(context.Background(), e.project)
	require.NoError(t, err)
	require.Len(t, exports, 1)
	assert.Equal(t, []string{"9:16", "720p", "vp9"}, []string{exports[0].AspectRatio, exports[0].Resolution, exports[0].Codec})
	file := filepath.Join(e.dir, "assets", filepath.FromSlash(exports[0].FilePath))
	info, err := e.tools.Probe(context.Background(), file)
	require.NoError(t, err)
	assert.Equal(t, [2]int{720, 1280}, [2]int{info.Width, info.Height})
	assert.True(t, info.HasAudio)
	assert.InDelta(t, 2, info.Duration, 0.3)
	entries, err := os.ReadDir(filepath.Dir(file))
	require.NoError(t, err)
	for _, en := range entries {
		assert.False(t, strings.HasPrefix(en.Name(), "."), "no temporary file remains: %s", en.Name())
	}
	rec = e.get("/projects/"+e.project+"/exports/"+exports[0].ID+"/video", false)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "video/webm", rec.Header().Get("Content-Type"))
	assert.Equal(t, []string{"sandbox", `attachment; filename="mam-export-` + exports[0].ExportedAt.UTC().Format("20060102") + `-` + exports[0].ID + `.webm"`}, []string{rec.Header().Get("Content-Security-Policy"), rec.Header().Get("Content-Disposition")}, "the download is saved under a name of its own, not `video`")

	// The completion notice carries no URL in its text; the chat draws the download link from ref_type / ref_id.
	var notice string
	require.NoError(t, e.db.Reader().QueryRow(`SELECT content FROM chat_message WHERE project_id = ? AND ref_type = 'export' AND ref_id = ?`, e.project, exports[0].ID).Scan(&notice))
	assert.NotContains(t, notice, "/projects/")
	assert.Contains(t, e.get("/projects/"+e.project, false).Body.String(), `<a href="/projects/`+e.project+`/exports/`+exports[0].ID+`/video"`)
}

func TestEditor_ReEditVerdictKeepsTheProjectEditing(t *testing.T) {
	e := newEditorApp(t)
	e.setState("VISUAL_DESIGN")
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/takes/t1/select", nil).Code)
	require.Equal(t, http.StatusOK, e.roughCut().Code)
	e.llm.answer = `{"verdict":"re_edit","findings":[{"clip_index":1,"issue":"冒頭が長い","action":"1 秒短くする"}]}`

	rec := e.form(http.MethodPost, "/edit-timelines/"+e.timeline().ID+"/judge", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "EDITING", e.state())
	j := e.judge()
	require.NotNil(t, j)
	assert.Equal(t, domain.EditorReEdit, j.Verdict)
	assert.Equal(t, "冒頭が長い", j.Findings[0].Issue)
	assert.Contains(t, rec.Body.String(), "冒頭が長い")
}

func TestEditor_SaveRejectsAClipOfAnotherShotsTakeAndAShortClip(t *testing.T) {
	e := newEditorApp(t)
	e.setState("VISUAL_DESIGN")
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/takes/t1/select", nil).Code)
	require.Equal(t, http.StatusOK, e.roughCut().Code)
	id := e.timeline().ID

	tooShort := `{"clips":[{"shot_id":"` + e.shotID + `","take_id":"t1","in_point":0,"out_point":0.1,"transition":{"type":"cut","seconds":0}}]}`
	rec := e.request(http.MethodPut, "/edit-timelines/"+id, []byte(tooShort), "application/json", false)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	unknownShot := `{"clips":[{"shot_id":"nope","take_id":"t1","in_point":0,"out_point":2,"transition":{"type":"cut","seconds":0}}]}`
	rec = e.request(http.MethodPut, "/edit-timelines/"+id, []byte(unknownShot), "application/json", false)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, 1, e.timeline().Version, "nothing was saved")
}

func TestEditor_FinalQCFailSendsTheProjectBackToTheStageWithTheProblem(t *testing.T) {
	e := newEditorApp(t)
	e.setState("VISUAL_DESIGN")
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/takes/t1/select", nil).Code)
	require.Equal(t, http.StatusOK, e.roughCut().Code)
	first := e.timeline()
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/edit-timelines/"+first.ID+"/judge", nil).Code)
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/projects/"+e.project+"/audio/generate", nil).Code)
	// The edit changes after the audio was made.
	body := `{"clips":[{"shot_id":"` + e.shotID + `","take_id":"t1","in_point":0,"out_point":2,"transition":{"type":"cut","seconds":0}}]}`
	require.Equal(t, http.StatusNoContent, e.request(http.MethodPut, "/edit-timelines/"+first.ID, []byte(body), "application/json", false).Code)
	assert.Equal(t, "EDITING", e.state(), "an edit takes the Project back to EDITING")
	e.setState("AUDIO")
	rec := e.form(http.MethodPost, "/projects/"+e.project+"/final-qc", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "EDITING", e.state())
	assert.Contains(t, rec.Body.String(), "data-qc-findings")
	assert.Equal(t, http.StatusConflict, e.form(http.MethodPost, "/projects/"+e.project+"/export", nil).Code, "not READY")
}

func TestEditor_AudioNeedsTheAudioStageAndExportNeedsReady(t *testing.T) {
	e := newEditorApp(t)
	e.setState("EDITING")
	assert.Equal(t, http.StatusConflict, e.form(http.MethodPost, "/projects/"+e.project+"/audio/generate", nil).Code)
	assert.Equal(t, http.StatusConflict, e.form(http.MethodPost, "/projects/"+e.project+"/final-qc", nil).Code)
	assert.Equal(t, http.StatusConflict, e.form(http.MethodPost, "/projects/"+e.project+"/export", nil).Code)
}

func TestEditor_ExportValidatesTheOptions(t *testing.T) {
	e := newEditorApp(t)
	e.setState("READY")
	rec := e.form(http.MethodPost, "/projects/"+e.project+"/export", url.Values{"codec": {"prores"}})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "コーデック")
}

func TestEditor_PageRendersTheTimelineEditorAndTheStages(t *testing.T) {
	e := newEditorApp(t)
	rec := e.get("/projects/"+e.project+"/edit", false)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `<mam-timeline-editor api-url="/api/v1/projects/`+e.project+`/edit-timeline">`)
	assert.Contains(t, rec.Body.String(), `id="editor-actions"`)
	for _, action := range []string{"/edit/rough-cut", "/audio/generate", "/final-qc", "/export"} {
		assert.Contains(t, rec.Body.String(), `hx-post="/projects/`+e.project+action+`"`, "the stage buttons post to routes that exist")
	}
	assert.Equal(t, http.StatusOK, e.get("/projects/"+e.project+"/export", false).Code)
	assert.Equal(t, http.StatusOK, e.get("/projects/"+e.project+"/edit/actions", true).Code)
	assert.Equal(t, http.StatusNotFound, e.get("/projects/nope/edit", false).Code)
}

func TestEditor_RoughCutCheckpointHoldsTheProjectUntilTheCardIsApproved(t *testing.T) {
	e := newEditorApp(t)
	e.setLevel(domain.ConfirmKeyCheck)
	e.setState("VISUAL_DESIGN")
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/takes/t1/select", nil).Code)
	require.Equal(t, http.StatusOK, e.roughCut().Code)

	rec := e.form(http.MethodPost, "/edit-timelines/"+e.timeline().ID+"/judge", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "AWAITING_APPROVAL", e.state())
	assert.Equal(t, http.StatusConflict, e.form(http.MethodPost, "/projects/"+e.project+"/audio/generate", nil).Code)
	card := lastID(t, e.get("/projects/"+e.project+"/messages", true).Body.String())
	rec = e.form(http.MethodPost, "/projects/"+e.project+"/approvals/"+card, url.Values{"decision": {"approve"}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "AUDIO", e.state())
}

// timelineOn is GET /api/v1/projects/:id/edit-timeline for a Branch ("" is the trunk).
func (e *editorApp) timelineOn(branchID string) (timelineBody, int) {
	e.t.Helper()
	rec := e.get("/api/v1/projects/"+e.project+"/edit-timeline?branch_id="+branchID, false)
	var b timelineBody
	if rec.Code == http.StatusOK {
		require.NoError(e.t, json.Unmarshal(rec.Body.Bytes(), &b))
	}
	return b, rec.Code
}

func TestEditor_EachBranchKeepsItsOwnTimelineAndCaptionsSurviveTheSave(t *testing.T) {
	e := newEditorApp(t)
	e.setState("VISUAL_DESIGN")
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/takes/t1/select", nil).Code)
	require.Equal(t, http.StatusOK, e.roughCut().Code)
	trunk := e.timeline()
	rec := e.form(http.MethodPost, "/shots/"+e.shotID+"/branch", url.Values{"name": {"逃げる"}})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	branchID := strings.TrimPrefix(rec.Header().Get("HX-Redirect"), "/branches/")
	_, code := e.timelineOn(branchID)
	assert.Equal(t, http.StatusNotFound, code, "a new Branch has no edit yet")

	// The Branch makes its own Rough Cut: its own version series, the trunk stays as it was.
	rec = e.form(http.MethodPost, "/projects/"+e.project+"/edit/rough-cut", url.Values{"branch_id": {branchID}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	onBranch, code := e.timelineOn(branchID)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, 1, onBranch.Version)
	assert.NotEqual(t, trunk.ID, onBranch.ID)
	assert.Contains(t, onBranch.SaveURL, onBranch.ID)

	// Editing the Branch (a caption, a shorter cut) appends to the Branch's series only.
	body := `{"clips":[{"shot_id":"` + e.shotID + `","take_id":"t1","kind":"main","caption":"  朝が来た ","in_point":0,"out_point":2,"transition":{"type":"cut","seconds":0}}]}`
	require.Equal(t, http.StatusNoContent, e.request(http.MethodPut, onBranch.SaveURL, []byte(body), "application/json", false).Code)
	edited, _ := e.timelineOn(branchID)
	assert.Equal(t, 2, edited.Version)
	assert.Equal(t, "朝が来た", edited.Clips[0].Caption)
	assert.Equal(t, "main", edited.Clips[0].Kind)
	assert.InDelta(t, 2, edited.Duration, 0.01)
	stillTrunk := e.timeline()
	assert.Equal(t, trunk.ID, stillTrunk.ID, "the trunk's latest version is untouched")
	assert.InDelta(t, trunk.Duration, stillTrunk.Duration, 1e-9)

	// A caption over the limit is refused and nothing is saved.
	tooLong := strings.Replace(body, "  朝が来た ", strings.Repeat("あ", domain.MaxCaptionRunes+1), 1)
	rec = e.request(http.MethodPut, edited.SaveURL, []byte(tooLong), "application/json", false)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	unchanged, _ := e.timelineOn(branchID)
	assert.Equal(t, 2, unchanged.Version)
	_, code = e.timelineOn("nope")
	assert.Equal(t, http.StatusNotFound, code)
}
