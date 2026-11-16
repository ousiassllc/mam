package chatscreen_test

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/audiofile"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/gentest/chat"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// attach posts a file to the route of the attachments, as the file inputs of the panel do (multipart, with the conversation and the kind).
func (s *screen) attach(kind, name string, data []byte) *httptest.ResponseRecorder {
	s.t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	require.NoError(s.t, w.WriteField("session", s.sess))
	require.NoError(s.t, w.WriteField("kind", kind))
	if name != "" {
		part, err := w.CreateFormFile("file", name)
		require.NoError(s.t, err)
		_, _ = part.Write(data)
	}
	require.NoError(s.t, w.Close())
	req := httptest.NewRequest(http.MethodPost, paths.GenerationTestChat("files"), &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("HX-Request", "true")
	req.AddCookie(s.cookie)
	req.Header.Set(middleware.CSRFHeaderName, s.token)
	return s.send(req)
}

// s2vStep estimates the speech-to-video model with the sound and the picture the user attached.
var s2vStep = `{"action":"estimate","text":"見積りです","request":{"media":"video","models":["` + string(gentesttest.S2VModel) + `"],"prompts":[{"prompt":"she says hello"}],"resolution":"720p","duration_seconds":5,"audio":true,"image":true}}`

// The panel has the two inputs; an attached sound and picture are told in the thread, the card names them, and the button runs a batch with the sound as its input.
func TestAttachedFilesGoFromTheInputsToTheCardAndTheBatch(t *testing.T) {
	s := newScreen(t, gentesttest.WithInputModels())
	page := s.get(paths.GenerationTestChat()).Body.String()
	assert.Contains(t, page, `data-gtc-attach="audio"`)
	assert.Contains(t, page, `data-gtc-attach="image"`)
	assert.Contains(t, page, `hx-post="/generation-test/chat/files"`)
	assert.Contains(t, page, `name="file"`)

	rec := s.attach("audio", "voice.wav", gentesttest.WAV)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "音声ファイルを添付しました: voice.wav")
	rec = s.attach("image", "face.png", gentesttest.PNG)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "参照画像を添付しました: face.png")

	s.llm.queue(s2vStep)
	rec = s.say("しゃべる動画を作って")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.Contains(t, body, `data-testid="gtc-proposal"`)
	assert.Contains(t, body, "音声ファイル", "the card names the files the batch uses")
	assert.Contains(t, body, "voice.wav（0.1 秒）")
	assert.Contains(t, body, "face.png")
	assert.Empty(t, s.rig.Fake.Calls(), "attaching and estimating generate nothing")

	rec = s.post(paths.GenerationTestChat("proposals", proposalOf(t, body), "confirm"), url.Values{"session": {s.sess}})
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	require.Eventually(t, func() bool { return len(s.rig.Fake.CallsOf(gentesttest.S2VModel)) == 1 }, 20*time.Second, 10*time.Millisecond)
	v := s.rig.Fake.CallsOf(gentesttest.S2VModel)[0].Video
	require.NotNil(t, v.Audio)
	assert.Equal(t, gentesttest.WAV, v.Audio.Data)
}

// The server reads the kind from the bytes: what is no sound (or no picture) is a 422 line in the thread with the form's words, whatever its name says.
func TestAFileOfTheWrongKindIsRefusedWith422(t *testing.T) {
	s := newScreen(t)
	rec := s.attach("audio", "voice.wav", []byte("this is a text"))
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "WAV・MP3・FLAC・OGG・M4A")
	rec = s.attach("image", "face.png", gentesttest.WAV)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "PNG・JPEG・WebP")
	rec = s.attach("video", "a.mp4", []byte("x"))
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "添付できるのは音声ファイルか参照画像です")
	rec = s.attach("audio", "", nil)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "no file")
	assert.Contains(t, rec.Body.String(), "ファイルを選んでください")
}

// A file over the limit of its kind is refused, however the body is cut: a sound over 50 MiB, and a picture over 20 MiB (413 when the whole body is over the biggest file).
func TestAFileOverTheLimitIsRefused(t *testing.T) {
	assert.EqualValues(t, gentest.MaxImageBytes, view.MaxImageMiB<<20, "the form says the limit the service holds")
	assert.EqualValues(t, audiofile.MaxBytes, view.AudioMaxMiB<<20)
	s := newScreen(t)
	rec := s.attach("image", "big.png", append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, gentest.MaxImageBytes)...))
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "a picture over 20 MiB")
	assert.Contains(t, rec.Body.String(), "20 MiB")

	rec = s.attach("audio", "huge.wav", append([]byte("RIFF\x00\x00\x00\x00WAVE"), make([]byte, audiofile.MaxBytes+2<<20)...))
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code, "a body over the biggest file is cut off before it is read")
	assert.True(t, strings.Contains(rec.Body.String(), "大きすぎます"), rec.Body.String())

	rec = s.attach("audio", "edge.wav", append([]byte("RIFF\x00\x00\x00\x00WAVE"), make([]byte, audiofile.MaxBytes)...))
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "one byte over 50 MiB inside the body limit is the service's refusal")
	assert.Contains(t, rec.Body.String(), "50 MiB")
}

// A conversation that was never opened takes a file (it starts one, like a message); an id of the wrong shape is refused.
func TestAttachingToAMalformedConversationIsRefused(t *testing.T) {
	s := newScreen(t)
	s.sess = "../etc"
	rec := s.attach("audio", "voice.wav", gentesttest.WAV)
	assert.Equal(t, http.StatusGone, rec.Code)
}
