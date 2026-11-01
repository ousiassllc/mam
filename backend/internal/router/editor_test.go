package router_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/testenv"
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
	return newEditorAppWith(t, nil)
}

// newEditorAppWith is newEditorApp with a last look at the Deps (nil: none).
func newEditorAppWith(t *testing.T, tweak func(*service.Deps)) *editorApp {
	t.Helper()
	l := &editorLLM{answer: `{"verdict":"pass","findings":[]}`, audioPlan: defaultAudioPlan}
	spoken := &[]domain.VoiceLine{}
	sample := &audiodirector.Sample{}
	speech := tone{spoken: spoken}
	ta := newTakeAppWith(t, func(d *service.Deps) {
		tools, err := media.Look()
		require.NoError(t, err)
		speech.data = testenv.FFmpegFile(t, ".wav", "-f", "lavfi", "-i", "sine=frequency=300:duration=1.5")
		d.LLM, d.Media = l, &tools
		d.Audio = audiodirector.Providers{Speech: speech, Music: speech, Sounds: speech, Cloner: voiceCloner{id: "cloned-voice-1", sample: sample}}
		if tweak != nil {
			tweak(d)
		}
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

func (e *editorApp) state() string { return e.stateOn(e.t) }

// stateOn, timelineFor and judgeOn fail c, not the test: an Eventually condition runs in its own goroutine (see countOn).
func (e *editorApp) stateOn(c require.TestingT) string {
	var s string
	require.NoError(c, e.db.Reader().QueryRowContext(context.Background(), `SELECT state FROM project WHERE id = ?`, e.project).Scan(&s))
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
	Clips    []struct {
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

func (e *editorApp) timeline() timelineBody { return e.timelineFor(e.t) }

func (e *editorApp) timelineFor(c require.TestingT) timelineBody {
	rec := e.get("/api/v1/projects/"+e.project+"/edit-timeline", false)
	require.Equal(c, http.StatusOK, rec.Code, rec.Body.String())
	var b timelineBody
	require.NoError(c, json.Unmarshal(rec.Body.Bytes(), &b))
	return b
}

// judge is the newest Editor Judge result of the current timeline version (nil: none yet). The
// result is not part of the timeline API: editorpanel.Actions (Templ) draws it.
func (e *editorApp) judge() *domain.EditorJudgeResult { return e.judgeOn(e.t) }
func (e *editorApp) judgeOn(c require.TestingT) *domain.EditorJudgeResult {
	res, err := repository.NewEditorRepository(e.db).LatestEditorJudge(context.Background(), e.timelineFor(c).ID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil
	}
	require.NoError(c, err)
	return &res
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
