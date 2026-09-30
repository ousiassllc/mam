package router_test

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// toAudio takes the editor flow to the point where audio can be made: a script with a narrated and
// a spoken line, a Rough Cut of the trunk and the AUDIO stage.
func toAudio(t *testing.T) *editorApp {
	t.Helper()
	e := newEditorApp(t)
	e.setState("VISUAL_DESIGN")
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/takes/t1/select", nil).Code)
	doc := domain.ScriptDoc{Script: domain.Script{Scenes: []domain.ScriptScene{{Title: "Street", Dialogue: []string{"ナレーション: 朝が来た", "ユイ: 行こう"}}}}}
	_, err := repository.NewPlanRepository(e.db).SaveScript(context.Background(), e.project, doc)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, e.roughCut().Code)
	e.setState("AUDIO")
	return e
}

func (e *editorApp) audioVersions(branchID string) []domain.AudioTimelineVersion {
	e.t.Helper()
	vs, err := repository.NewEditorRepository(e.db).ListAudioTimelines(context.Background(), e.project, branchID)
	require.NoError(e.t, err)
	return vs
}

func TestAudio_EachBranchKeepsItsOwnAudioTimelineVersions(t *testing.T) {
	e := toAudio(t)
	rec := e.form(http.MethodPost, "/shots/"+e.shotID+"/branch", url.Values{"name": {"逃げる"}})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	branchID := strings.TrimPrefix(rec.Header().Get("HX-Redirect"), "/branches/")
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/projects/"+e.project+"/edit/rough-cut", url.Values{"branch_id": {branchID}}).Code)
	e.setState("AUDIO")

	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/projects/"+e.project+"/audio/generate", nil).Code)
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/projects/"+e.project+"/audio/generate", url.Values{"branch_id": {branchID}}).Code)
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/projects/"+e.project+"/audio/generate", nil).Code)

	trunk, branch := e.audioVersions(""), e.audioVersions(branchID)
	require.Len(t, trunk, 2)
	require.Len(t, branch, 1)
	assert.Equal(t, []int{2, 1}, []int{trunk[0].VersionNumber, trunk[1].VersionNumber})
	assert.Equal(t, 1, branch[0].VersionNumber)
	assert.Equal(t, branchID, branch[0].BranchID)
	edit, err := repository.NewEditorRepository(e.db).LatestEditTimeline(context.Background(), e.project, branchID)
	require.NoError(t, err)
	assert.Equal(t, edit.ID, branch[0].Timeline.EditTimelineID, "the Branch audio was made for the Branch's own edit")
}

func TestAudio_AnUnknownBranchIsNotFound(t *testing.T) {
	e := toAudio(t)

	rec := e.form(http.MethodPost, "/projects/"+e.project+"/audio/generate", url.Values{"branch_id": {"no-such-branch"}})

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Empty(t, e.audioVersions(""))
}

func TestAudio_TheQualityPolicyDecidesHowMuchIsMade(t *testing.T) {
	e := toAudio(t)
	e.execSQL(`UPDATE project SET quality_policy = 'fast' WHERE id = ?`, e.project)
	e.llm.audioPlan = `{"scenes":[{"index":1,"emotion":"期待","bgm":"warm guitar","ambience":"birds","effects":[{"layer":"se","at_seconds":0.2,"duration_seconds":1,"prompt":"chime"}]}]}`

	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/projects/"+e.project+"/audio/generate", nil).Code)

	tl := e.audioVersions("")[0].Timeline
	assert.Equal(t, domain.QualityFast, tl.Quality)
	assert.Equal(t, 1, tl.Count(domain.LayerBGM))
	assert.Zero(t, tl.Count(domain.LayerSE)+tl.Count(domain.LayerAmbience), "FAST makes speech and BGM only")
}

func TestAudio_ABudgetCeilingThatCannotPayForTheSpeechIsRefusedAndNothingIsRecorded(t *testing.T) {
	e := toAudio(t)
	e.execSQL(`UPDATE project SET project_budget_ceiling = 0.0001 WHERE id = ?`, e.project)

	rec := e.form(http.MethodPost, "/projects/"+e.project+"/audio/generate", nil)

	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "予算")
	assert.Empty(t, e.audioVersions(""))
	assert.Empty(t, *e.spoken, "no provider is called over the budget")
	var n int
	require.NoError(t, e.db.Reader().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM cost_ledger_entry WHERE provider LIKE 'elevenlabs-%'`).Scan(&n))
	assert.Zero(t, n)
}

func TestAudio_AClonedVoiceSpeaksTheLinesOfItsSpeaker(t *testing.T) {
	e := toAudio(t)
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	require.NoError(t, w.WriteField("speaker", "ユイ"))
	part, err := w.CreateFormFile("sample", "yui.mp3")
	require.NoError(t, err)
	_, err = part.Write([]byte("sample-recording"))
	require.NoError(t, err)
	require.NoError(t, w.Close())

	rec := e.request(http.MethodPost, "/projects/"+e.project+"/audio/voices", buf.Bytes(), w.FormDataContentType(), true)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "ユイ", "the panel lists the cloned voice")
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/projects/"+e.project+"/audio/generate", nil).Code)

	voices := map[string]string{}
	for _, l := range *e.spoken {
		voices[l.Speaker+"/"+string(l.Kind)] = l.VoiceID
	}
	assert.Equal(t, "cloned-voice-1", voices["ユイ/dialogue"])
	assert.Empty(t, voices["/narration"], "Narration keeps its default voice")
}

func TestAudio_CloneVoiceValidatesTheRequest(t *testing.T) {
	e := toAudio(t)
	post := func(speaker string, sample []byte) int {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		require.NoError(t, w.WriteField("speaker", speaker))
		if sample != nil {
			part, err := w.CreateFormFile("sample", "s.mp3")
			require.NoError(t, err)
			_, err = part.Write(sample)
			require.NoError(t, err)
		}
		require.NoError(t, w.Close())
		return e.request(http.MethodPost, "/projects/"+e.project+"/audio/voices", buf.Bytes(), w.FormDataContentType(), true).Code
	}

	assert.Equal(t, http.StatusUnprocessableEntity, post("  ", []byte("x")), "a speaker is required")
	assert.Equal(t, http.StatusUnprocessableEntity, post("ユイ", nil), "a sample is required")
	assert.Equal(t, http.StatusUnprocessableEntity, post("ユイ", []byte{}), "an empty sample is refused")
	assert.Equal(t, http.StatusUnprocessableEntity, post(strings.Repeat("あ", 21), []byte("x")), "a long speaker name is refused")
}
