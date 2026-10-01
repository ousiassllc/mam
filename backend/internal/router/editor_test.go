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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// editorLLM answers the Editor Judge with a fixed JSON reply and the Audio Director's Planner with
// audioPlan (a plan for Scene 1).
type editorLLM struct {
	answer    string
	audioPlan string
	// audioErr, when set, fails the Audio Director's Planner call.
	audioErr error
}

const defaultAudioPlan = `{"scenes":[{"index":1,"emotion":"期待","bgm":"warm acoustic guitar","ambience":"","effects":[{"layer":"se","at_seconds":0.2,"duration_seconds":1,"prompt":"soft chime"}]}]}`

func (l *editorLLM) Complete(_ context.Context, agent llm.Agent, _ llm.Request) (llm.Response, error) {
	if agent == llm.AgentAudioDirector {
		if l.audioErr != nil {
			return llm.Response{}, l.audioErr
		}
		return llm.Response{Text: l.audioPlan}, nil
	}
	return llm.Response{Text: l.answer}, nil
}

// tone is every ElevenLabs model in one fake: it "speaks", composes and generates a 1.5 s tone.
type tone struct {
	data []byte
	// spoken collects the lines it was asked to speak.
	spoken *[]domain.VoiceLine
}

func (t tone) Synthesize(_ context.Context, line domain.VoiceLine) (audiodirector.Speech, error) {
	*t.spoken = append(*t.spoken, line)
	return audiodirector.Speech{Data: t.data, Ext: "wav"}, nil
}

func (t tone) Compose(context.Context, string, float64) (audiodirector.Speech, error) {
	return audiodirector.Speech{Data: t.data, Ext: "wav"}, nil
}

func (t tone) Generate(context.Context, string, float64, bool) (audiodirector.Speech, error) {
	return audiodirector.Speech{Data: t.data, Ext: "wav"}, nil
}

type editorApp struct {
	*takeApp
	llm *editorLLM
	// spoken are the lines the fake text-to-speech was asked for.
	spoken *[]domain.VoiceLine
}

// voiceCloner answers a clone request with a fixed voice and remembers the sample.
type voiceCloner struct {
	id     string
	sample *audiodirector.Sample
}

func (c voiceCloner) Clone(_ context.Context, _ string, s audiodirector.Sample) (string, error) {
	*c.sample = s
	return c.id, nil
}

func newEditorApp(t *testing.T) *editorApp {
	t.Helper()
	l := &editorLLM{answer: `{"verdict":"pass","findings":[]}`, audioPlan: defaultAudioPlan}
	spoken := &[]domain.VoiceLine{}
	sample := &audiodirector.Sample{}
	speech := tone{spoken: spoken}
	ta := newTakeAppWith(t, func(d *service.Deps) {
		tools, err := media.Look()
		require.NoError(t, err)
		wav := filepath.Join(t.TempDir(), "voice.wav")
		out, err := exec.Command("ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", "sine=frequency=300:duration=1.5", wav).CombinedOutput()
		require.NoError(t, err, string(out))
		speech.data, err = os.ReadFile(wav)
		require.NoError(t, err)
		d.LLM, d.Media = l, &tools
		d.Audio = audiodirector.Providers{Speech: speech, Music: speech, Sounds: speech, Cloner: voiceCloner{id: "cloned-voice-1", sample: sample}}
	})
	e := &editorApp{takeApp: ta, llm: l, spoken: spoken}
	e.setLevel(domain.ConfirmFullAuto) // no approval card unless a test asks for one
	// The tests run the stages one by one; the AUTO chain is tested with setMode("auto").
	e.setMode("director")
	// DRAMA asks nothing of a one-clip cut that a test could trip over (PRODUCT / CM wants the product on screen).
	e.execSQL(`UPDATE project SET production_template = 'drama' WHERE id = ?`, e.project)
	return e
}

func (e *editorApp) setLevel(level domain.ConfirmationLevel) {
	e.t.Helper()
	rec := e.form(http.MethodPut, "/projects/"+e.project+"/confirmation-level", url.Values{"level": {string(level)}})
	require.Equal(e.t, http.StatusOK, rec.Code, rec.Body.String())
}

func (e *editorApp) setMode(mode string) {
	e.execSQL(`UPDATE project SET mode = ? WHERE id = ?`, mode, e.project)
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
		Kind       string  `json:"kind"`
		Caption    string  `json:"caption"`
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
	var providers []string
	rows, err := e.db.Reader().QueryContext(context.Background(), `SELECT provider FROM cost_ledger_entry WHERE project_id = ? AND provider LIKE 'elevenlabs-%' ORDER BY provider`, e.project)
	require.NoError(t, err)
	for rows.Next() {
		var p string
		require.NoError(t, rows.Scan(&p))
		providers = append(providers, p)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{"elevenlabs-music", "elevenlabs-sfx", "elevenlabs-tts", "elevenlabs-tts"}, providers)
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

	var body struct {
		Judge struct {
			Verdict  string `json:"verdict"`
			Findings []struct {
				Need struct {
					Kind      string  `json:"kind"`
					AfterClip int     `json:"after_clip"`
					Seconds   float64 `json:"duration_seconds"`
				} `json:"need_shot"`
			} `json:"findings"`
		} `json:"judge"`
		RequestShotURL string `json:"request_shot_url"`
	}
	require.NoError(t, json.Unmarshal(e.get("/api/v1/projects/"+e.project+"/edit-timeline", false).Body.Bytes(), &body))
	assert.Equal(t, "need_shot", body.Judge.Verdict)
	require.Len(t, body.Judge.Findings, 1)
	assert.Equal(t, "reaction", body.Judge.Findings[0].Need.Kind)
	assert.Equal(t, 1, body.Judge.Findings[0].Need.AfterClip)
	assert.Equal(t, "/edit-timelines/"+id+"/request-additional-shot", body.RequestShotURL)

	// Without generation configured (no video model) the Director cannot be asked.
	assert.Equal(t, http.StatusServiceUnavailable, e.form(http.MethodPost, body.RequestShotURL, nil).Code)
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

	require.Eventually(t, func() bool { return e.state() == "READY" }, 60*time.Second, 50*time.Millisecond, "state: %s", e.state())
	assert.Equal(t, "pass", e.timeline().Judge.Verdict)
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

	require.Eventually(t, func() bool {
		rec := e.get("/api/v1/projects/"+e.project+"/edit-timeline", false)
		return rec.Code == http.StatusOK && e.timeline().Judge != nil
	}, 30*time.Second, 50*time.Millisecond)
	assert.Equal(t, "re_edit", e.timeline().Judge.Verdict)
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
	assert.Nil(t, e.timeline().Judge, "no false Re-edit finding is stored")
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
