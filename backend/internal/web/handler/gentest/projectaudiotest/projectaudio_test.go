// Package projectaudiotest_test tests the audio files of a Project on the generation test screen (Issue #1153): the picker next to the file input of the sound, the
// row a picked file makes, and the run with it. It has its own small harness because the directory of the handlers is full (linterly).
package projectaudiotest_test

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/audiofile"
	"github.com/ousiassllc/mam/backend/internal/audiofile/audiofiletest"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/router/settingsroutes"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.ReleaseMode)
	m.Run()
}

type screen struct {
	t      *testing.T
	rig    *gentesttest.Rig
	engine http.Handler
	cookie *http.Cookie
	token  string
}

func newScreen(t *testing.T) *screen {
	t.Helper()
	rig := gentesttest.New(t, gentesttest.WithInputModels())
	svc := servicetest.New(t, service.Deps{DB: rig.DB, Credentials: credentialtest.New()})
	settings := settingsroutes.New(t.TempDir()+"/llm.json", false, t.TempDir()+"/m.json", false)
	settings.Gentest = rig.Svc
	s := &screen{t: t, rig: rig, engine: router.New(svc, router.Options{Settings: settings})}
	rec := s.send(httptest.NewRequest(http.MethodGet, paths.GenerationTest(), nil))
	require.Equal(t, http.StatusOK, rec.Code)
	for _, c := range rec.Result().Cookies() {
		if c.Name == middleware.CSRFCookieName {
			s.cookie = c
		}
	}
	require.NotNil(t, s.cookie)
	m := regexp.MustCompile(`<meta name="csrf-token" content="([^"]+)"`).FindStringSubmatch(rec.Body.String())
	require.Len(t, m, 2)
	s.token = m[1]
	return s
}

func (s *screen) send(req *http.Request) *httptest.ResponseRecorder {
	req.Host, req.RemoteAddr = "127.0.0.1:8787", "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	s.engine.ServeHTTP(rec, req)
	return rec
}

// fragment is the GET an HTMX request makes.
func (s *screen) fragment(path string) string {
	s.t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("HX-Request", "true")
	req.AddCookie(s.cookie)
	rec := s.send(req)
	require.Equal(s.t, http.StatusOK, rec.Code, rec.Body.String())
	return rec.Body.String()
}

func (s *screen) post(path string, form url.Values) *httptest.ResponseRecorder {
	s.t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.AddCookie(s.cookie)
	req.Header.Set(middleware.CSRFHeaderName, s.token)
	return s.send(req)
}

// asset registers a file as an asset of the Project, the way an attachment of the chat is stored.
func (s *screen) asset(project string, typ domain.AssetType, ext, mime string, data []byte) string {
	s.t.Helper()
	id := domain.NewID()
	rel := filepath.Join(project, id+ext)
	require.NoError(s.t, os.MkdirAll(filepath.Join(s.rig.AssetsDir, project), 0o700))
	require.NoError(s.t, os.WriteFile(filepath.Join(s.rig.AssetsDir, rel), data, 0o600))
	require.NoError(s.t, s.rig.Repo.AddProjectAsset(context.Background(), domain.Asset{
		ID: id, ProjectID: project, Type: typ, FilePath: filepath.ToSlash(rel), MimeType: mime, SizeBytes: int64(len(data)),
	}))
	return id
}

// The video form offers the Projects next to the file input of the sound, the picker lists the audio files of the chosen Project (as players, never the pictures),
// and an image form offers none of it.
func TestTheVideoFormPicksTheSoundFromTheAudioFilesOfAProject(t *testing.T) {
	s := newScreen(t)
	project := s.rig.NewProject()
	wav := s.asset(project, domain.AssetAudio, ".wav", audiofile.WAV, audiofiletest.WAV(16000, 16000))
	s.asset(project, domain.AssetImage, ".png", "image/png", gentesttest.PNG)
	s.asset(s.rig.NewProject(), domain.AssetAudio, ".mp3", audiofile.MP3, audiofiletest.MP3(true, 20))

	form := s.fragment(paths.GenerationTestPart("form") + "?media=video")
	assert.Contains(t, form, `data-testid="gt-audio-project"`)
	assert.Contains(t, form, `value="`+project+`"`, "the Projects are the choices")
	assert.NotContains(t, s.fragment(paths.GenerationTestPart("form")+"?media=image"), `data-testid="gt-audio-project"`)

	empty := s.fragment(paths.GenerationTestPart("picker") + "?kind=audio")
	assert.Contains(t, empty, "プロジェクトを選ぶと、その音声ファイルが並びます")

	list := s.fragment(paths.GenerationTestPart("picker") + "?kind=audio&audio_project=" + project)
	assert.Equal(t, 1, strings.Count(list, `data-testid="gt-audio-pick"`), "only the audio file of this Project")
	assert.Contains(t, list, `<audio controls preload="none"`)
	assert.Contains(t, list, `src="/projects/`+project+`/assets/`+wav+`"`)
	assert.NotContains(t, list, "<img")
	assert.Contains(t, html.UnescapeString(list), `ref=project_asset:`+project+`:`+wav, "the button asks for the row of this asset")
	assert.Contains(t, list, "role=audio")

	none := s.fragment(paths.GenerationTestPart("picker") + "?kind=audio&audio_project=" + s.rig.NewProject())
	assert.Contains(t, none, `data-testid="gt-audio-picker-empty"`)
	req := httptest.NewRequest(http.MethodGet, paths.GenerationTestPart("picker")+"?kind=audio&audio_project=nope", nil)
	req.Header.Set("HX-Request", "true")
	req.AddCookie(s.cookie)
	assert.Equal(t, http.StatusNotFound, s.send(req).Code, "an unknown Project is refused")

	pictures := s.fragment(paths.GenerationTestPart("picker") + "?project=" + project)
	assert.NotContains(t, pictures, wav, "the picture picker never lists a sound")
}

// The row a picked audio file makes is a player with the role audio and a × (no picture, no strength), and a picture or a file that is no audio asset of the
// Project makes none.
func TestAPickedAudioFileIsARowOfTheSoundAndOnlyAnAudioAssetMakesOne(t *testing.T) {
	s := newScreen(t)
	project := s.rig.NewProject()
	wav := s.asset(project, domain.AssetAudio, ".wav", audiofile.WAV, audiofiletest.WAV(16000, 16000))
	png := s.asset(project, domain.AssetImage, ".png", "image/png", gentesttest.PNG)
	row := func(id string) (*httptest.ResponseRecorder, string) {
		q := url.Values{"ref": {"project_asset:" + project + ":" + id}, "role": {"audio"}, "media": {"video"}, "model": {string(gentesttest.S2VModel)}}
		req := httptest.NewRequest(http.MethodGet, paths.GenerationTestPart("ref-row")+"?"+q.Encode(), nil)
		req.Header.Set("HX-Request", "true")
		req.AddCookie(s.cookie)
		rec := s.send(req)
		return rec, rec.Body.String()
	}

	rec, body := row(wav)
	require.Equal(t, http.StatusOK, rec.Code, body)
	assert.Contains(t, body, `<audio controls preload="none"`)
	assert.Contains(t, body, `value="audio"`, "the role is posted")
	assert.Contains(t, body, "data-gt-remove")
	for _, absent := range []string{"<img", "data-gt-zoom", "data-gt-strength", "<select"} {
		assert.NotContains(t, body, absent)
	}

	rec, _ = row(png)
	assert.Equal(t, http.StatusNotFound, rec.Code, "a picture is no sound")
	rec, _ = row("nope")
	assert.Equal(t, http.StatusNotFound, rec.Code)

	// without the role a picture row is as before: a picture of the Project is a row of the pictures
	q := url.Values{"ref": {"project_asset:" + project + ":" + png}, "media": {"video"}, "model": {string(gentesttest.S2VModel)}}
	req := httptest.NewRequest(http.MethodGet, paths.GenerationTestPart("ref-row")+"?"+q.Encode(), nil)
	req.Header.Set("HX-Request", "true")
	req.AddCookie(s.cookie)
	pic := s.send(req)
	assert.Equal(t, http.StatusOK, pic.Code)
	assert.NotContains(t, pic.Body.String(), "<audio")
}

// The estimate, the confirmation and the run take the sound from the Project: the model gets the bytes of the asset, and the batch says where they came from.
func TestTheSoundOfAProjectGoesFromTheFormToTheModel(t *testing.T) {
	s := newScreen(t)
	project := s.rig.NewProject()
	data := audiofiletest.FLAC(44100, 44100)
	flac := s.asset(project, domain.AssetAudio, ".flac", audiofile.FLAC, data)
	pic, _, err := s.rig.Svc.StageReference(gentesttest.PNG)
	require.NoError(t, err)
	audioRef := "project_asset:" + project + ":" + flac
	form := url.Values{
		"media": {"video"}, "model": {string(gentesttest.S2VModel)}, "prompt": {"a person talks"}, "count": {"1"},
		"ref": {"upload::" + pic, audioRef}, "role_upload::" + pic: {"reference"}, "role_" + audioRef: {"audio"},
	}
	est := s.post(paths.GenerationTestPart("estimate"), form)
	require.Equal(t, http.StatusOK, est.Code, est.Body.String())
	held := url.Values{}
	for _, m := range regexp.MustCompile(`<input type="hidden" name="([^"]*)" value="([^"]*)"`).FindAllStringSubmatch(est.Body.String(), -1) {
		held.Add(html.UnescapeString(m[1]), html.UnescapeString(m[2]))
	}
	require.NotEmpty(t, held.Get("estimate_id"))
	assert.Contains(t, held["ref"], audioRef, "the confirmation holds the Project's asset as the sound")

	run := s.post(paths.GenerationTestPart("run"), held)
	require.Equal(t, http.StatusAccepted, run.Code, run.Body.String())
	id := regexp.MustCompile(`data-batch="([^"]+)"`).FindStringSubmatch(run.Body.String())
	require.Len(t, id, 2, run.Body.String())
	b := s.rig.Wait(id[1])
	require.Len(t, b.Results, 1)
	require.Equal(t, "succeeded", b.Results[0].Status, b.Results[0].Error)

	calls := s.rig.Fake.CallsOf(gentesttest.S2VModel)
	require.Len(t, calls, 1)
	require.NotNil(t, calls[0].Video.Audio)
	assert.Equal(t, data, calls[0].Video.Audio.Data)
	assert.Equal(t, audiofile.FLAC, calls[0].Video.Audio.MimeType)

	// a picture of the Project named as the sound is refused with the words that say so, and nothing is sent
	png := s.asset(project, domain.AssetImage, ".png", "image/png", gentesttest.PNG)
	bad := url.Values{}
	for k, v := range form {
		bad[k] = v
	}
	badRef := "project_asset:" + project + ":" + png
	pic2, _, err := s.rig.Svc.StageReference(gentesttest.PNG) // the first picture was taken by the run
	require.NoError(t, err)
	bad["ref"], bad["role_"+badRef] = []string{"upload::" + pic2, badRef}, []string{"audio"}
	bad["role_upload::"+pic2] = []string{"reference"}
	rec := s.post(paths.GenerationTestPart("estimate"), bad)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, html.UnescapeString(rec.Body.String()), "音声ファイルではありません")
	assert.Len(t, s.rig.Fake.CallsOf(gentesttest.S2VModel), 1, "nothing more was sent")
}
