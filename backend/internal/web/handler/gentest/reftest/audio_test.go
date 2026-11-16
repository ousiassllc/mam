package reftest_test

import (
	"bytes"
	"html"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// The audio file of a video form (Issue #1153): it is staged with the audio rules, priced, carried to "実行" and shown in the result, as a player.

// file is a file of a multipart form: its field, its name and its bytes.
type file struct {
	field, name string
	data        []byte
}

// postFiles posts the form with the files, as the browser's multipart form does.
func (s *screen) postFiles(path string, form url.Values, files ...file) *httptest.ResponseRecorder {
	s.t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, vs := range form {
		for _, v := range vs {
			require.NoError(s.t, w.WriteField(k, v))
		}
	}
	for _, f := range files {
		part, err := w.CreateFormFile(f.field, f.name)
		require.NoError(s.t, err)
		_, _ = part.Write(f.data)
	}
	require.NoError(s.t, w.Close())
	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("HX-Request", "true")
	req.AddCookie(s.cookie)
	req.Header.Set(middleware.CSRFHeaderName, s.token)
	return s.send(req)
}

// s2vForm is the form of a speech-to-video model with its reference picture (the file number 1 of the field upload).
func s2vForm(models ...string) url.Values {
	return url.Values{
		"media": {"video"}, "model": models, "prompt": {"a person talks"}, "count": {"1"},
		"ref": {"upload::#1"}, "role_upload::#1": {"reference"},
	}
}

var (
	pictureFile = file{"upload", "face.png", gentesttest.PNG}
	audioFile   = file{"upload_audio", "voice.wav", gentesttest.WAV}
)

// The audio goes with the estimate to the confirmation and from there to the model: the confirmation holds it as a staged audio input (so "実行" runs the
// same request), and the model gets the bytes of the file with the audio type.
func TestTheAudioFileOfTheFormIsEstimatedConfirmedAndSentToTheModel(t *testing.T) {
	s := newScreen(t, gentesttest.WithInputModels())
	rec := s.postFiles(paths.GenerationTestPart("estimate"), s2vForm(string(gentesttest.S2VModel)), pictureFile, audioFile)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	form := hiddenOf(t, rec.Body.String())

	var audioRef string
	for _, ref := range form["ref"] {
		if form.Get("role_"+ref) == gentest.RoleAudio {
			audioRef = ref
		}
	}
	require.NotEmpty(t, audioRef, "the confirmation carries the audio as an input with the role audio: %v", form)
	assert.True(t, strings.HasPrefix(audioRef, "upload::") && strings.HasSuffix(audioRef, ".wav"), "a staged file that keeps its kind: %s", audioRef)
	assert.Len(t, form["ref"], 2, "the picture and the audio")

	run := s.run(form)
	require.Equal(t, http.StatusAccepted, run.Code, run.Body.String())
	batchID := regexp.MustCompile(`data-batch="([^"]+)"`).FindStringSubmatch(run.Body.String())
	require.Len(t, batchID, 2, run.Body.String())
	b := s.rig.Wait(batchID[1])
	require.Len(t, b.Results, 1)
	require.Equal(t, "succeeded", b.Results[0].Status, b.Results[0].Error)

	calls := s.rig.Fake.CallsOf(gentesttest.S2VModel)
	require.Len(t, calls, 1)
	require.NotNil(t, calls[0].Video.Audio, "the audio reached the model")
	assert.Equal(t, gentesttest.WAV, calls[0].Video.Audio.Data)
	assert.Equal(t, "audio/wav", calls[0].Video.Audio.MimeType)
	assert.Len(t, calls[0].Video.References, 1, "the picture is the reference, not the audio")
}

// What is none of the five kinds (WAV, MP3, FLAC, OGG, M4A) is refused with the words that name them, whatever its name says, and nothing is sent.
func TestAnAudioFileThatIsNotOneOfTheFiveKindsIsRefused(t *testing.T) {
	s := newScreen(t, gentesttest.WithInputModels())
	for name, data := range map[string][]byte{"a picture named .wav": gentesttesPNG(), "text": []byte("hello, this is not sound")} {
		rec := s.postFiles(paths.GenerationTestPart("estimate"), s2vForm(string(gentesttest.S2VModel)), pictureFile, file{"upload_audio", "voice.wav", data})
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, name)
		assert.Contains(t, html.UnescapeString(rec.Body.String()), "WAV・MP3・FLAC・OGG・M4A のどれか", name)
	}
	assert.Empty(t, s.rig.Fake.Calls())
}

func gentesttesPNG() []byte { return gentesttest.PNG }

// A speech-to-video model with no audio is stopped with the common error that names the audio, and the picture alone does not satisfy it.
func TestEstimateStopsASpeechToVideoModelWithoutAudio(t *testing.T) {
	s := newScreen(t, gentesttest.WithInputModels())
	rec := s.postFiles(paths.GenerationTestPart("estimate"), s2vForm(string(gentesttest.S2VModel)), pictureFile)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), `data-error-code="input_missing"`)
	assert.Empty(t, s.rig.Fake.Calls())
}

// The notice under the inputs is the same check, asked while the form is filled in: the browser posts a chosen file as text, and that is enough to say "this
// model needs audio" (none chosen), "this model leaves it out" (a model that uses none), and nothing (a model that uses it).
func TestTheInputCheckKnowsTheChosenAudioFile(t *testing.T) {
	s := newScreen(t, gentesttest.WithInputModels())
	check := func(form url.Values) string {
		req := httptest.NewRequest(http.MethodPost, paths.GenerationTestPart("input-check"), strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")
		req.AddCookie(s.cookie)
		req.Header.Set(middleware.CSRFHeaderName, s.token)
		rec := s.send(req)
		require.Equal(t, http.StatusOK, rec.Code)
		return rec.Body.String()
	}
	s2v, t2v := string(gentesttest.S2VModel), string(gentesttest.T2VModel)

	none := check(s2vForm(s2v))
	assert.Contains(t, none, `data-error-code="input_missing"`, "the picture is there, the audio is not")

	chosen := s2vForm(s2v)
	chosen.Set("upload_audio", "[object File]")
	assert.NotContains(t, check(chosen), `data-error-code=`, "the audio is chosen")

	unused := s2vForm(s2v, t2v)
	unused.Set("upload_audio", "[object File]")
	body := check(unused)
	assert.Contains(t, body, `data-testid="gt-input-warnings"`, "the T2V model leaves the audio out: the amber notice")
	assert.NotContains(t, body, `data-error-code=`)
}

// The audio shows in the form as a player with a way to take it out, with no picture, strength or zoom; the file input names the same field the handler reads.
func TestAnAudioInputOfTheFormIsARowWithAPlayerNotAPicture(t *testing.T) {
	s := newScreen(t, gentesttest.WithInputModels())
	ref, err := s.rig.Confirmed(gentest.Request{
		Media: "video", Models: []string{string(gentesttest.S2VModel)}, Prompts: []gentest.PromptInput{{Prompt: "a person talks"}},
		References: s.stagedRefs(gentest.RoleReference, gentest.RoleAudio),
	})
	require.NoError(t, err)
	d, err := s.rig.Svc.Result(t.Context(), s.rig.Wait(ref.BatchID).Results[0].ID)
	require.NoError(t, err)
	var picture, audio string
	for _, in := range d.Inputs {
		if in.Role == gentest.RoleAudio {
			audio = "batch_input::" + in.ID
		} else {
			picture = "batch_input::" + in.ID
		}
	}
	require.NotEmpty(t, audio)

	q := url.Values{"media": {"video"}, "model": {string(gentesttest.S2VModel)}, "ref": {picture, audio}, "role_" + picture: {"reference"}, "role_" + audio: {"audio"}}
	body := s.fragment(paths.GenerationTestPart("form") + "?" + q.Encode())

	rows := regexp.MustCompile(`(?s)<li[^>]*data-gt-ref data-ref="batch_input.*?</li>`).FindAllString(body, -1)
	require.Len(t, rows, 2, "the two rows of the form, not the template of the browser's own")
	audioRow := rows[1]
	assert.Contains(t, audioRow, `<audio controls preload="none"`)
	assert.Contains(t, audioRow, "アップロードした音声（前の生成の入力）")
	assert.Contains(t, audioRow, `name="role_`+audio+`" value="audio"`, "the role is posted, so the estimate sees an audio input")
	assert.Contains(t, audioRow, "data-gt-remove")
	for _, absent := range []string{"<img", "data-gt-zoom", "data-gt-strength", "data-gt-move", "<select"} {
		assert.NotContains(t, audioRow, absent)
	}
	assert.Contains(t, rows[0], "<img", "the picture keeps its thumbnail")
	assert.Contains(t, body, `type="file" name="upload_audio" accept="audio/wav,audio/mpeg,audio/flac,audio/ogg,audio/mp4,.wav,.mp3,.flac,.ogg,.m4a"`)
}

// The page of a result plays the audio it was made from, and the audio a model was not given is not a picture either.
func TestTheResultPageShowsAudioInputsAsPlayers(t *testing.T) {
	s := newScreen(t, gentesttest.WithInputModels())
	ref, err := s.rig.Confirmed(gentest.Request{
		Media: "video", Models: []string{string(gentesttest.S2VModel), string(gentesttest.T2VModel)}, Prompts: []gentest.PromptInput{{Prompt: "a person talks"}},
		References: s.stagedRefs(gentest.RoleReference, gentest.RoleAudio),
	})
	require.NoError(t, err)
	b := s.rig.Wait(ref.BatchID)
	require.Len(t, b.Results, 2)

	for _, r := range b.Results {
		d, err := s.rig.Svc.Result(t.Context(), r.ID)
		require.NoError(t, err)
		var audioID string
		for _, in := range d.Inputs {
			if in.Role == gentest.RoleAudio {
				audioID = in.ID
			}
		}
		require.NotEmpty(t, audioID)
		src := paths.GenerationTestPart("inputs", audioID, "file")
		page := s.page(paths.GenerationTestPart("results", r.ID))
		assert.Contains(t, page, `<audio controls preload="none" class="w-full" src="`+src+`"`, d.ModelLabel)
		assert.NotContains(t, page, `<img class="h-24 w-full rounded-md object-cover" src="`+src, "no picture for sound: "+d.ModelLabel)
		assert.NotContains(t, page, `src="`+src+`" alt=`, d.ModelLabel)
		assert.Contains(t, page, "音声・", "the role in words: "+d.ModelLabel)
		if d.Model == string(gentesttest.T2VModel) { // it uses neither the picture nor the audio: both are listed as not given
			unused := regexp.MustCompile(`(?s)data-testid="gt-unused".*?</ul>`).FindString(page)
			require.NotEmpty(t, unused)
			assert.Contains(t, unused, "<audio")
			assert.Equal(t, 1, strings.Count(unused, "<img"), "only the picture is drawn")
		}
	}
}

// stagedRefs stages a picture and a WAV and names them with the roles.
func (s *screen) stagedRefs(pictureRole, audioRole string) []gentest.RefInput {
	s.t.Helper()
	pic, _, err := s.rig.Svc.StageReference(gentesttest.PNG)
	require.NoError(s.t, err)
	aud, _, err := s.rig.Svc.StageAudio(gentesttest.WAV)
	require.NoError(s.t, err)
	return []gentest.RefInput{
		{Source: gentest.FromStaged, ID: pic, Role: pictureRole},
		{Source: gentest.FromStaged, ID: aud, Role: audioRole},
	}
}

// The video form offers the audio file and tells which model needs it on its card (and only that one).
func TestTheVideoFormOffersTheAudioFileAndTheCardOfTheModelThatNeedsIt(t *testing.T) {
	s := newScreen(t, gentesttest.WithInputModels())
	body := s.fragment(paths.GenerationTestPart("form") + "?media=video")
	assert.Contains(t, body, "音声ファイル（S2V・WAV・MP3・FLAC・OGG・M4A）")
	assert.Contains(t, body, "50 MiB まで")
	assert.Equal(t, 1, strings.Count(body, `data-testid="gt-model-audio"`), "the S2V model only")
	assert.NotContains(t, s.fragment(paths.GenerationTestPart("form")+"?media=image"), `name="upload_audio"`, "an image form has no audio")
}
