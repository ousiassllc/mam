package router_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// editorLLM answers the Editor Judge with a fixed JSON reply.
type editorLLM struct{ answer string }

func (l *editorLLM) Complete(context.Context, llm.Agent, llm.Request) (llm.Response, error) {
	return llm.Response{Text: l.answer}, nil
}

// tone is a Synthesizer that "speaks" a 1.5 s tone.
type tone struct{ data []byte }

func (t tone) Synthesize(context.Context, domain.VoiceLine) (audiodirector.Speech, error) {
	return audiodirector.Speech{Data: t.data, Ext: "wav"}, nil
}

type editorApp struct {
	*takeApp
	llm *editorLLM
}

func newEditorApp(t *testing.T) *editorApp {
	t.Helper()
	l := &editorLLM{answer: `{"verdict":"pass","findings":[]}`}
	var speech tone
	ta := newTakeAppWith(t, func(d *service.Deps) {
		tools, err := media.Look()
		require.NoError(t, err)
		wav := filepath.Join(t.TempDir(), "voice.wav")
		out, err := exec.Command("ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", "sine=frequency=300:duration=1.5", wav).CombinedOutput()
		require.NoError(t, err, string(out))
		speech.data, err = os.ReadFile(wav)
		require.NoError(t, err)
		d.LLM, d.Speech, d.Media = l, speech, &tools
	})
	e := &editorApp{takeApp: ta, llm: l}
	e.setLevel(domain.ConfirmFullAuto) // no approval card unless a test asks for one
	return e
}

func (e *editorApp) setLevel(level domain.ConfirmationLevel) {
	e.t.Helper()
	rec := e.form(http.MethodPut, "/projects/"+e.project+"/confirmation-level", url.Values{"level": {string(level)}})
	require.Equal(e.t, http.StatusOK, rec.Code, rec.Body.String())
}

// setState puts the Project in a state the pipeline would have reached.
func (e *editorApp) setState(state string) {
	e.execSQL(`UPDATE project SET state = ? WHERE id = ?`, state, e.project)
}

func (e *editorApp) state() string {
	e.t.Helper()
	var s string
	require.NoError(e.t, e.db.Reader().QueryRowContext(context.Background(), `SELECT state FROM project WHERE id = ?`, e.project).Scan(&s))
	return s
}

func (e *editorApp) roughCut() *httptest.ResponseRecorder {
	return e.form(http.MethodPost, "/projects/"+e.project+"/edit/rough-cut", nil)
}

type timelineBody struct {
	ID       string  `json:"id"`
	Version  int     `json:"version_number"`
	Duration float64 `json:"duration_seconds"`
	SaveURL  string  `json:"save_url"`
	Readonly bool    `json:"readonly"`
	Judge    *struct {
		Verdict  string `json:"verdict"`
		Findings []struct {
			ClipIndex int    `json:"clip_index"`
			Issue     string `json:"issue"`
		} `json:"findings"`
	} `json:"judge"`
	Clips []struct {
		ShotID     string  `json:"shot_id"`
		TakeID     string  `json:"take_id"`
		Label      string  `json:"label"`
		In         float64 `json:"in_point"`
		Out        float64 `json:"out_point"`
		Transition struct {
			Type    string  `json:"type"`
			Seconds float64 `json:"seconds"`
		} `json:"transition"`
	} `json:"clips"`
}

func (e *editorApp) timeline() timelineBody {
	e.t.Helper()
	rec := e.get("/api/v1/projects/"+e.project+"/edit-timeline", false)
	require.Equal(e.t, http.StatusOK, rec.Code, rec.Body.String())
	var b timelineBody
	require.NoError(e.t, json.Unmarshal(rec.Body.Bytes(), &b))
	return b
}

func TestEditor_RoughCutNeedsASelectedTakeForEveryShot(t *testing.T) {
	e := newEditorApp(t)
	e.setState("VISUAL_DESIGN")

	rec := e.form(http.MethodPost, "/projects/"+e.project+"/edit/rough-cut", nil)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "Selected Take")
	assert.Equal(t, http.StatusNotFound, e.get("/api/v1/projects/"+e.project+"/edit-timeline", false).Code)
}

func TestEditor_RoughCutRefusesAProjectThatIsNotAtTheEditingStage(t *testing.T) {
	e := newEditorApp(t)
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/takes/t1/select", nil).Code)

	rec := e.form(http.MethodPost, "/projects/"+e.project+"/edit/rough-cut", nil) // still DRAFT

	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "DRAFT")
}

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

	// Editor Judge: PASS moves the Project to AUDIO (the default level asks for no Rough Cut card).
	rec = e.form(http.MethodPost, "/edit-timelines/"+saved.ID+"/judge", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "AUDIO", e.state())
	assert.Equal(t, "pass", e.timeline().Judge.Verdict)

	// Audio: two lines are spoken, mixed and normalized.
	rec = e.form(http.MethodPost, "/projects/"+e.project+"/audio/generate", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "セリフ 2 行")
	latest, err := repository.NewEditorRepository(e.db).LatestAudioTimeline(context.Background(), e.project)
	require.NoError(t, err)
	audio := latest.Timeline
	require.Len(t, audio.Clips, 2)
	assert.InDelta(t, domain.DefaultTargetLUFS, audio.MeasuredLUFS, 1.5)
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
	j := e.timeline().Judge
	require.NotNil(t, j)
	assert.Equal(t, "re_edit", j.Verdict)
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

	rec := e.get("/projects/"+e.project+"/editor", false)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `<mam-timeline-editor api-url="/api/v1/projects/`+e.project+`/edit-timeline">`)
	assert.Contains(t, rec.Body.String(), `id="editor-actions"`)
	assert.Equal(t, http.StatusNotFound, e.get("/projects/nope/editor", false).Code)
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
