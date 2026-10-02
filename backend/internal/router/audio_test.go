package router_test

import (
	"bytes"
	"context"
	"encoding/json"
	"html"
	"mime/multipart"
	"net/http"
	"net/url"
	"regexp"
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

func TestExport_ABranchIsExportedFromItsOwnTimelinesAndTheRecordSaysSo(t *testing.T) {
	e := toAudio(t)
	rec := e.form(http.MethodPost, "/shots/"+e.shotID+"/branch", url.Values{"name": {"逃げる"}})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	branchID := strings.TrimPrefix(rec.Header().Get("HX-Redirect"), "/branches/")
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/projects/"+e.project+"/edit/rough-cut", url.Values{"branch_id": {branchID}}).Code)
	e.setState("AUDIO")
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/projects/"+e.project+"/audio/generate", url.Values{"branch_id": {branchID}}).Code)
	e.setState("READY")
	exportURL := "/projects/" + e.project + "/export"

	// Only the Branch has an Audio Timeline: the trunk cannot be exported, an unknown Branch neither.
	assert.Equal(t, http.StatusNotFound, e.form(http.MethodPost, exportURL, nil).Code)
	assert.Equal(t, http.StatusNotFound, e.form(http.MethodPost, exportURL, url.Values{"branch_id": {"no-such-branch"}}).Code)
	exports, err := repository.NewEditorRepository(e.db).ListExports(context.Background(), e.project)
	require.NoError(t, err)
	assert.Empty(t, exports)

	rec = e.form(http.MethodPost, exportURL, url.Values{"branch_id": {branchID}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	exports, err = repository.NewEditorRepository(e.db).ListExports(context.Background(), e.project)
	require.NoError(t, err)
	require.Len(t, exports, 1)
	assert.Equal(t, branchID, exports[0].BranchID)
}

func TestExport_ABranchWhoseAudioWasMadeForAnOlderEditIsRefused(t *testing.T) {
	e := toAudio(t)
	rec := e.form(http.MethodPost, "/shots/"+e.shotID+"/branch", url.Values{"name": {"逃げる"}})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	branchID := strings.TrimPrefix(rec.Header().Get("HX-Redirect"), "/branches/")
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/projects/"+e.project+"/edit/rough-cut", url.Values{"branch_id": {branchID}}).Code)
	e.setState("AUDIO")
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/projects/"+e.project+"/audio/generate", url.Values{"branch_id": {branchID}}).Code)

	// The Branch's edit is saved again after its audio was made: the audio belongs to the older edit.
	onBranch, code := e.timelineOn(branchID)
	require.Equal(t, http.StatusOK, code)
	body := `{"clips":[{"shot_id":"` + e.shotID + `","take_id":"t1","kind":"main","in_point":0,"out_point":2,"transition":{"type":"cut","seconds":0}}]}`
	require.Equal(t, http.StatusNoContent, e.request(http.MethodPut, onBranch.SaveURL, []byte(body), "application/json", false).Code)
	e.setState("READY")

	rec = e.form(http.MethodPost, "/projects/"+e.project+"/export", url.Values{"branch_id": {branchID}})
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	exports, err := repository.NewEditorRepository(e.db).ListExports(context.Background(), e.project)
	require.NoError(t, err)
	assert.Empty(t, exports, "no video is written from a stale audio")
}

func TestExport_TheTrunkWhoseAudioWasMadeForAnOlderEditIsRefused(t *testing.T) {
	e := toAudio(t)
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/projects/"+e.project+"/audio/generate", nil).Code)

	// The trunk's edit is saved again after its audio was made (a Reshoot round put in late): the
	// audio belongs to the older edit, and the Project is READY all the same.
	onTrunk, code := e.timelineOn("")
	require.Equal(t, http.StatusOK, code)
	body := `{"clips":[{"shot_id":"` + e.shotID + `","take_id":"t1","kind":"main","in_point":0,"out_point":2,"transition":{"type":"cut","seconds":0}}]}`
	require.Equal(t, http.StatusNoContent, e.request(http.MethodPut, onTrunk.SaveURL, []byte(body), "application/json", false).Code)
	e.setState("READY")

	rec := e.form(http.MethodPost, "/projects/"+e.project+"/export", nil)
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	exports, err := repository.NewEditorRepository(e.db).ListExports(context.Background(), e.project)
	require.NoError(t, err)
	assert.Empty(t, exports, "no video is written from a stale audio")
}

func TestExport_TheBranchEditorPageExportsItsBranchAndKeepsTheScope(t *testing.T) {
	e := toAudio(t)
	rec := e.form(http.MethodPost, "/shots/"+e.shotID+"/branch", url.Values{"name": {"逃げる"}})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	branchID := strings.TrimPrefix(rec.Header().Get("HX-Redirect"), "/branches/")
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/projects/"+e.project+"/edit/rough-cut", url.Values{"branch_id": {branchID}}).Code)
	e.setState("AUDIO")
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/projects/"+e.project+"/audio/generate", url.Values{"branch_id": {branchID}}).Code)
	e.setState("READY")

	// What htmx sends: the form's hx-vals on top of its fields.
	page := e.get("/projects/"+e.project+"/edit?branch_id="+branchID, false).Body.String()
	tag := regexp.MustCompile(`<form[^>]*hx-post="/projects/` + e.project + `/export"[^>]*>`).FindString(page)
	require.NotEmpty(t, tag, "the Export form is on the page")
	vals := regexp.MustCompile(`hx-vals="([^"]*)"`).FindStringSubmatch(tag)
	require.NotNil(t, vals, "the Export form sends hx-vals: %s", tag)
	var sent map[string]string
	require.NoError(t, json.Unmarshal([]byte(html.UnescapeString(vals[1])), &sent))
	require.Equal(t, map[string]string{"branch_id": branchID}, sent)

	rec = e.form(http.MethodPost, "/projects/"+e.project+"/export", url.Values{"branch_id": {sent["branch_id"]}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	exports, err := repository.NewEditorRepository(e.db).ListExports(context.Background(), e.project)
	require.NoError(t, err)
	require.Len(t, exports, 1)
	assert.Equal(t, branchID, exports[0].BranchID)
	assert.Contains(t, rec.Body.String(), `hx-get="/projects/`+e.project+`/edit/actions?branch_id=`+branchID+`"`, "the response keeps the Branch scope")
}

func TestFinalQC_TheBranchEditorPageKeepsItsBranchScopeInTheAnswer(t *testing.T) {
	e := toAudio(t)
	rec := e.form(http.MethodPost, "/shots/"+e.shotID+"/branch", url.Values{"name": {"逃げる"}})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	branchID := strings.TrimPrefix(rec.Header().Get("HX-Redirect"), "/branches/")
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/projects/"+e.project+"/edit/rough-cut", url.Values{"branch_id": {branchID}}).Code)
	e.setState("AUDIO")
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/projects/"+e.project+"/audio/generate", nil).Code)
	e.setState("AUDIO")
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/projects/"+e.project+"/audio/generate", url.Values{"branch_id": {branchID}}).Code)
	e.setState("AUDIO")

	// What htmx sends: the button's hx-vals.
	page := e.get("/projects/"+e.project+"/edit?branch_id="+branchID, false).Body.String()
	tag := regexp.MustCompile(`<button[^>]*hx-post="/projects/` + e.project + `/final-qc"[^>]*>`).FindString(page)
	require.NotEmpty(t, tag, "the Final QC button is on the page")
	vals := regexp.MustCompile(`hx-vals="([^"]*)"`).FindStringSubmatch(tag)
	require.NotNil(t, vals, "the Final QC button sends hx-vals: %s", tag)
	var sent map[string]string
	require.NoError(t, json.Unmarshal([]byte(html.UnescapeString(vals[1])), &sent))
	require.Equal(t, map[string]string{"branch_id": branchID}, sent)
	assert.Contains(t, page, "data-qc-trunk-note", "the Branch panel says Final QC checks the trunk")

	rec = e.form(http.MethodPost, "/projects/"+e.project+"/final-qc", url.Values{"branch_id": {sent["branch_id"]}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.Contains(t, body, `hx-get="/projects/`+e.project+`/edit/actions?branch_id=`+branchID+`"`, "the response keeps the Branch scope")
	assert.Contains(t, body, `hx-post="/projects/`+e.project+`/audio/generate"`)
	assert.NotContains(t, body, `hx-vals="{&#34;branch_id&#34;:&#34;&#34;}"`, "no later button falls back to the trunk")

	trunk := e.get("/projects/"+e.project+"/edit", false).Body.String()
	assert.NotContains(t, trunk, "data-qc-trunk-note")
}

// A Branch panel offers Final QC by the audio of the trunk, which is what the server inspects (#340).
func TestFinalQC_TheBranchPanelFollowsTheAudioOfTheTrunk(t *testing.T) {
	e := toAudio(t)
	rec := e.form(http.MethodPost, "/shots/"+e.shotID+"/branch", url.Values{"name": {"逃げる"}})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	branchID := strings.TrimPrefix(rec.Header().Get("HX-Redirect"), "/branches/")
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/projects/"+e.project+"/edit/rough-cut", url.Values{"branch_id": {branchID}}).Code)
	e.setState("AUDIO")
	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/projects/"+e.project+"/audio/generate", url.Values{"branch_id": {branchID}}).Code)
	e.setState("AUDIO")

	finalQC := regexp.MustCompile(`<button[^>]*hx-post="/projects/` + e.project + `/final-qc"[^>]*>`)
	disabled := regexp.MustCompile(`\sdisabled[\s=>]`)
	page := e.get("/projects/"+e.project+"/edit?branch_id="+branchID, false).Body.String()
	assert.Regexp(t, disabled, finalQC.FindString(page), "the Branch has audio, the trunk has none: Final QC would be refused")
	assert.Contains(t, page, "data-qc-trunk-missing")
	assert.Equal(t, http.StatusConflict, e.form(http.MethodPost, "/projects/"+e.project+"/final-qc", url.Values{"branch_id": {branchID}}).Code)

	require.Equal(t, http.StatusOK, e.form(http.MethodPost, "/projects/"+e.project+"/audio/generate", nil).Code)
	e.setState("AUDIO")
	page = e.get("/projects/"+e.project+"/edit?branch_id="+branchID, false).Body.String()
	assert.NotRegexp(t, disabled, finalQC.FindString(page))
	assert.NotContains(t, page, "data-qc-trunk-missing")
}
