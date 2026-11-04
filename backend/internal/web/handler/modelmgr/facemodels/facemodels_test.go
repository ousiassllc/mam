package facemodels_test

import (
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

	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/facesim"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/router/settingsroutes"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/facemodels/facemodelstest"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.ReleaseMode)
	mmtest.Main(m)
}

// screen is the model manager screen with the face check models service of a fake GitHub, driven as the browser's HTMX does.
type screen struct {
	t      *testing.T
	fm     *facemodelstest.Rig
	engine http.Handler
	cookie *http.Cookie
	token  string
}

func newScreen(t *testing.T, withModels bool) *screen {
	t.Helper()
	rig := mmtest.New(t)
	s := &screen{t: t, fm: facemodelstest.New(t)}
	if withModels {
		rig.Gentest.FaceModels = s.fm.Svc
	}
	svc := servicetest.New(t, service.Deps{DB: testdb.Open(t), Credentials: credentialtest.New()})
	settings := settingsroutes.New(filepath.Join(t.TempDir(), "llm.json"), false, filepath.Join(t.TempDir(), "m.json"), false)
	settings.ModelManager, settings.Gentest = rig.Svc, rig.Gentest
	s.engine = router.New(svc, router.Options{Settings: settings})

	rec := s.send(httptest.NewRequest(http.MethodGet, paths.ModelManager(), nil))
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

func (s *screen) do(method, path string, form url.Values) *httptest.ResponseRecorder {
	s.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.AddCookie(s.cookie)
	req.Header.Set(middleware.CSRFHeaderName, s.token)
	return s.send(req)
}

func (s *screen) page() string {
	s.t.Helper()
	req := httptest.NewRequest(http.MethodGet, paths.ModelManager(), nil)
	req.AddCookie(s.cookie)
	rec := s.send(req)
	require.Equal(s.t, http.StatusOK, rec.Code)
	return rec.Body.String()
}

var (
	install  = paths.ModelManagerPart("face-models", "install")
	section  = paths.ModelManagerPart("face-models")
	progress = paths.ModelManagerPart("face-models", "progress")
)

// Issue #1026: the model manager lists the face check models as their own section, with the source, the licenses and the commercial-use answers (and the notice
// about the training data) before anything is installed, and an install button.
func TestTheScreenOffersTheFaceCheckModelsWithTheirLicensesBeforeTheInstall(t *testing.T) {
	s := newScreen(t, true)
	body := s.page()

	assert.Contains(t, body, `data-testid="mm-face-models"`)
	assert.Contains(t, body, "顔チェック用モデル")
	assert.Contains(t, body, `data-state="missing"`)
	assert.Contains(t, body, facesim.DetectorFile)
	assert.Contains(t, body, facesim.RecognizerFile)
	assert.Contains(t, body, "https://github.com/opencv/opencv_zoo", "the source")
	assert.Contains(t, body, "Apache-2.0", "the license")
	assert.Contains(t, body, "学習データの権利は確かめられていません", "the notice of the license is shown with the answer")
	assert.Contains(t, body, `data-testid="mm-face-models-install"`)
	assert.Contains(t, body, `data-testid="mm-face-models-accept"`)
	assert.NotContains(t, body, `data-testid="mm-face-models-remove"`, "nothing to remove yet")
	assert.Contains(t, body, filepath.Clean(s.fm.Dir), "where the files go")
}

// An app without the face models service leaves the section out.
func TestTheSectionIsLeftOutWithoutTheService(t *testing.T) {
	s := newScreen(t, false)
	assert.NotContains(t, s.page(), "mm-face-models")
}

// The install button installs: without the box ticked the section comes back with the refusal and nothing is fetched; ticked, the files are in place, the section says
// "導入済み" and the face engine reads them at once.
func TestTheInstallButtonInstallsAndTheEngineWorksWithoutARestart(t *testing.T) {
	s := newScreen(t, true)

	rec := s.do(http.MethodPost, install, url.Values{})
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "同意してから導入してください")
	assert.Zero(t, s.fm.Requests.Load())
	assert.False(t, facesim.Usable(s.fm.Engine))

	rec = s.do(http.MethodPost, install, url.Values{"accept": {"on"}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.Contains(t, body, `data-state="installed"`)
	assert.Contains(t, body, "導入しました")
	assert.Contains(t, body, "再起動は要りません")
	assert.NotContains(t, body, `data-testid="mm-face-models-install"`)
	assert.Contains(t, body, `data-testid="mm-face-models-remove"`)
	assert.True(t, facesim.Usable(s.fm.Engine))
	_, err := os.Stat(filepath.Join(s.fm.Dir, facesim.RecognizerFile))
	require.NoError(t, err)

	rec = s.do(http.MethodDelete, section, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `data-state="missing"`)
	assert.Contains(t, rec.Body.String(), "削除しました")
	assert.False(t, facesim.Usable(s.fm.Engine))
}

// With the models in place and the ONNX Runtime library missing, the section says the library is what is missing (installing the models again does not fix that).
func TestTheSectionSaysTheRuntimeIsMissingWhenTheModelsAreInstalled(t *testing.T) {
	s := newScreen(t, true)
	s.fm.RuntimeMissing.Store(true)

	rec := s.do(http.MethodPost, install, url.Values{"accept": {"on"}})
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, `data-state="installed"`)
	assert.Contains(t, body, `data-testid="mm-face-models-runtime"`)
	assert.Contains(t, body, "ONNX Runtime")
	assert.Contains(t, body, "まだオフ")
}

// The progress line is empty while no install runs.
func TestTheProgressLineIsEmptyWhenNothingRuns(t *testing.T) {
	s := newScreen(t, true)
	rec := s.do(http.MethodGet, progress, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `data-testid="mm-face-models-progress"`)
	assert.NotContains(t, rec.Body.String(), "ダウンロード中")
}
