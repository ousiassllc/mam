package voiceroutes_test

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
	"github.com/ousiassllc/mam/backend/internal/service/voicetrain"
)

// refusingRuntime is a PC where GPT-SoVITS is not installed: a training is refused with the reason of the runtime before anything starts.
type refusingRuntime struct{}

func (*refusingRuntime) BeginTool(context.Context, string, ...string) (voicetrain.Held, error) {
	return nil, localruntime.ErrNotInstalled
}

// seconds10 says every recording is 10 seconds long (ffprobe is not needed to test the routes).
type seconds10 struct{}

func (seconds10) AudioDuration(context.Context, string) (float64, error) { return 10, nil }

func (a *app) post(path string, fields map[string]string, name string, data []byte) *httptest.ResponseRecorder {
	a.t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		require.NoError(a.t, w.WriteField(k, v))
	}
	if name != "" {
		part, err := w.CreateFormFile("file", name)
		require.NoError(a.t, err)
		_, err = part.Write(data)
		require.NoError(a.t, err)
	}
	require.NoError(a.t, w.Close())
	return a.request(http.MethodPost, path, buf.Bytes(), w.FormDataContentType())
}

func TestVoiceTraining_ThePagesLoadThePanelAndTheReferenceVoiceSaveReloadsIt(t *testing.T) {
	a := newApp(t)
	_, seriesID, canonID := a.fixture()
	panel := "/series/" + seriesID + "/characters/" + canonID + "/voice-training"

	page := a.request(http.MethodGet, "/series/"+seriesID, nil, "").Body.String()
	assert.Contains(t, page, `id="voice-train-`+canonID+`"`)
	assert.Contains(t, page, `hx-get="`+panel+`"`)
	assert.Contains(t, page, `hx-trigger="load, voice-reference-changed from:body"`)

	world := a.request(http.MethodGet, "/projects/p1/world-state", nil, "").Body.String()
	assert.Contains(t, world, `hx-get="/projects/p1/characters/c1/voice-training"`)

	rec := a.put("/projects/p1/characters/c1/voice-reference", "voice.wav", wav(64), map[string]string{"text": "こんにちは"})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "voice-reference-changed", rec.Header().Get("HX-Trigger"), "the panel of the Character loads itself again")
	assert.NotContains(t, rec.Body.String(), "voice-train-", "the answer is the voice form alone: the panel is not drawn twice")
}

func TestVoiceTraining_ThePanelTellsTheConsentTheDataAndThatNoTimeCanBeToldYet(t *testing.T) {
	a := newApp(t)
	a.fixture()

	rec := a.request(http.MethodGet, "/projects/p1/characters/c1/voice-training", nil, "")

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.Contains(t, body, `data-testid="voice-train-consent-notice"`)
	assert.Contains(t, body, "声の持ち主本人の同意を得た音声だけで学習してください")
	assert.Contains(t, body, `data-testid="voice-train-need-reference"`, "no reference voice yet")
	assert.Contains(t, body, "学習に使う音声（0 個")
	assert.Contains(t, body, `data-testid="voice-train-eta-note"`)
	assert.Contains(t, body, "学習を始める前には出せません")
	assert.Contains(t, body, `disabled="disabled"`, "a training cannot start without the data")
	assert.NotContains(t, body, "hx-trigger=\"every", "nothing runs: no polling")
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	assert.Equal(t, http.StatusNotFound, a.request(http.MethodGet, "/projects/p1/characters/ghost/voice-training", nil, "").Code)
	assert.Equal(t, http.StatusNotFound, a.request(http.MethodGet, "/projects/ghost/characters/c1/voice-training", nil, "").Code)
}

func TestVoiceTraining_AClipIsAddedListedAndRemovedAndTheAnswerIsThePanelAlone(t *testing.T) {
	a := newApp(t)
	a.fixture()
	a.put("/projects/p1/characters/c1/voice-reference", "voice.wav", wav(64), map[string]string{"text": "こんにちは"})
	route := "/projects/p1/characters/c1/voice-training"

	rec := a.post(route+"/clips", map[string]string{"text": "今日はいい天気ですね"}, "a.wav", wav(64))

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.Contains(t, body, `id="voice-train-c1"`)
	assert.Contains(t, body, "今日はいい天気ですね")
	assert.Contains(t, body, "学習に使う音声（1 個・合計 10.0 秒）")
	assert.Contains(t, body, "音声が合わせて 10 秒しかありません", "10 seconds of the minute a training takes")
	assert.NotContains(t, body, "<html")
	id := regexp.MustCompile(`hx-delete="` + route + `/clips/([^"]+)"`).FindStringSubmatch(body)
	require.Len(t, id, 2)

	assert.Equal(t, http.StatusUnprocessableEntity, a.post(route+"/clips", map[string]string{"text": " "}, "b.wav", wav(64)).Code, "no transcript")
	assert.Equal(t, http.StatusUnprocessableEntity, a.post(route+"/clips", map[string]string{"text": "x"}, "b.txt", []byte("not audio")).Code)
	assert.Equal(t, http.StatusUnprocessableEntity, a.post(route+"/clips", map[string]string{"text": "x"}, "", nil).Code, "no file")

	rec = a.request(http.MethodDelete, route+"/clips/"+id[1], nil, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "学習に使う音声（0 個")
	assert.Equal(t, http.StatusNotFound, a.request(http.MethodDelete, route+"/clips/"+id[1], nil, "").Code)
	a.post(route+"/clips", map[string]string{"text": "もう一度"}, "c.wav", wav(64))
	rec = a.request(http.MethodDelete, route+"/clips", nil, "")
	assert.Contains(t, rec.Body.String(), "学習に使う音声（0 個")
}

func TestVoiceTraining_ASeriesCanonCharacterHasTheSameRoutes(t *testing.T) {
	a := newApp(t)
	_, seriesID, canonID := a.fixture()
	a.put("/series/"+seriesID+"/characters/"+canonID+"/voice-reference", "voice.wav", wav(64), nil)
	route := "/series/" + seriesID + "/characters/" + canonID + "/voice-training"

	rec := a.post(route+"/clips", map[string]string{"text": "こんにちは"}, "a.wav", wav(64))

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `hx-post="`+route+`/clips"`)
	assert.Equal(t, http.StatusNotFound, a.request(http.MethodGet, "/series/ghost/characters/"+canonID+"/voice-training", nil, "").Code)
}

func TestVoiceTraining_AStartThatCannotRunAnswersWithTheReasonAndStartsNothing(t *testing.T) {
	a := newApp(t)
	a.fixture()
	a.put("/projects/p1/characters/c1/voice-reference", "voice.wav", wav(64), map[string]string{"text": "こんにちは"})
	route := "/projects/p1/characters/c1/voice-training"

	rec := a.post(route, nil, "", nil)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "no clips: the reason is the data")
	assert.Contains(t, rec.Body.String(), "学習に使う音声がありません")

	for range 6 {
		require.Equal(t, http.StatusOK, a.post(route+"/clips", map[string]string{"text": "こんにちは"}, "a.wav", wav(64)).Code)
	}
	rec = a.post(route, map[string]string{"epochs_gpt": "abc"}, "", nil)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "エポック数は整数")

	rec = a.post(route, map[string]string{"epochs_gpt": "99"}, "", nil)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "GPT のエポック数")

	rec = a.post(route, nil, "", nil)
	assert.Equal(t, http.StatusConflict, rec.Code, "the runtime is not installed")
	assert.Contains(t, rec.Body.String(), "GPT-SoVITS")

	rec = a.post(route+"/cancel", nil, "", nil)
	assert.Equal(t, http.StatusConflict, rec.Code, "nothing runs")
	assert.Equal(t, http.StatusOK, a.request(http.MethodDelete, route+"/voice", nil, "").Code, "no trained voice is nothing to remove")
}
