package facefusionscreen_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/facefusion"
	"github.com/ousiassllc/mam/backend/internal/media/locate"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/router/settingsroutes"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/facefusionsettings"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// screen is the settings screen with the FaceFusion section over files of the test's temporary directory only.
type screen struct {
	t      *testing.T
	file   string
	engine http.Handler
	cookie *http.Cookie
	token  string
}

func newScreen(t *testing.T) *screen {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the layout uses POSIX paths")
	}
	file := filepath.Join(t.TempDir(), facefusionsettings.FileName)
	// Only the files of the temporary directory count: the machine's own python and FaceFusion, if it has them, are not found.
	finder := facefusion.Finder{Finder: locate.Finder{GOOS: "linux", Path: "/nowhere", Usable: func(p string) bool {
		return strings.HasPrefix(p, os.TempDir()) && locate.IsExecutable(p)
	}}, Dirs: []string{"/nowhere/facefusion"}}
	svc := servicetest.New(t, service.Deps{DB: testdb.Open(t), Credentials: credentialtest.New()})
	settings := settingsroutes.New(filepath.Join(t.TempDir(), "llm.json"), false, filepath.Join(t.TempDir(), "models.json"), false)
	settings.FaceFusion = facefusionsettings.New(file, "", facefusionsettings.WithFinder(finder))
	s := &screen{t: t, file: file, engine: router.New(svc, router.Options{Settings: settings})}
	rec := s.send(httptest.NewRequest(http.MethodGet, paths.Settings(), nil))
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

func (s *screen) get(path string) string {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.AddCookie(s.cookie)
	rec := s.send(req)
	require.Equal(s.t, http.StatusOK, rec.Code)
	return rec.Body.String()
}

func (s *screen) put(form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPut, paths.FaceFusionSettings(), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.AddCookie(s.cookie)
	req.Header.Set(middleware.CSRFHeaderName, s.token)
	return s.send(req)
}

// install writes a FaceFusion folder and a python in a temporary directory and returns their paths.
func install(t *testing.T) (dir, python string) {
	t.Helper()
	root := t.TempDir()
	dir = filepath.Join(root, "facefusion")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "facefusion"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "facefusion.py"), nil, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "facefusion", "metadata.py"), []byte("METADATA =\\\n{\n\t'version': '3.9.2',\n\t'license': 'OpenRAIL-AS',\n}\n"), 0o600))
	python = filepath.Join(root, "python")
	require.NoError(t, os.WriteFile(python, []byte("#!/bin/sh\n"), 0o600))
	require.NoError(t, os.Chmod(python, 0o700)) // #nosec G302 -- the test file has to be executable
	return dir, python
}

func TestPageSaysNothingIsFoundListsTheModelsWithTheirLicensesAndTheCautions(t *testing.T) {
	s := newScreen(t)

	body := s.get(paths.FaceFusionSettings())

	assert.Contains(t, body, `data-testid="facefusion-state-python"`)
	assert.Contains(t, body, "見つかりません")
	assert.Contains(t, body, "未設定")
	assert.Contains(t, body, "/nowhere/facefusion", "where it looks")
	assert.Contains(t, body, `data-testid="facefusion-model-ghost_2_256"`)
	assert.Contains(t, body, `data-testid="facefusion-model-inswapper_128"`)
	assert.Contains(t, body, "商用NG")
	assert.Contains(t, body, "研究用のみ")
	assert.Contains(t, body, "商用不明")
	assert.Contains(t, body, facefusion.AdultNote, "the adult pictures it declines")
	assert.Contains(t, body, facefusion.ArcFaceNote)
	assert.Contains(t, body, "OpenRAIL-AS")
	assert.Contains(t, body, facefusion.LicensesURL, "the primary source of the licenses")
	assert.Contains(t, s.get(paths.Settings()), `href="/settings/facefusion"`, "the overview has the card")
}

func TestSavedPathsAreShownWrittenAndUsedAtOnceAndClearedAgain(t *testing.T) {
	s := newScreen(t)
	dir, python := install(t)

	rec := s.put(url.Values{"python": {python}, "path": {dir}})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `id="facefusion-panel"`, "the answer is the section, to swap in place")
	assert.Contains(t, rec.Body.String(), "次の顔の入れ替えから使われます")
	assert.Contains(t, rec.Body.String(), "使えます")
	assert.Contains(t, rec.Body.String(), "FaceFusion 3.9.2（OpenRAIL-AS）")
	assert.Contains(t, s.get(paths.Settings()), "FaceFusion を使えます")
	b, err := os.ReadFile(s.file) // #nosec G304 -- a file of the test's temp directory
	require.NoError(t, err)
	assert.Contains(t, string(b), dir)

	rec = s.put(url.Values{"python": {""}, "path": {""}})

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "パスの指定を解除しました")
	assert.NoFileExists(t, s.file)
}

func TestAPathThatCannotBeUsedIsRefusedWithItsReasonAndKeptInTheField(t *testing.T) {
	s := newScreen(t)
	dir, python := install(t)
	empty := t.TempDir()

	for name, tc := range map[string]struct {
		form   url.Values
		reason string
	}{
		"folder without facefusion.py": {url.Values{"python": {python}, "path": {empty}}, "facefusion.py がありません"},
		"relative python":              {url.Values{"python": {"python"}, "path": {dir}}, "絶対パスではありません"},
		"python that is gone":          {url.Values{"python": {filepath.Join(empty, "nope")}}, "実在する実行ファイルではありません"},
	} {
		t.Run(name, func(t *testing.T) {
			rec := s.put(tc.form)

			require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
			assert.Contains(t, rec.Body.String(), `data-testid="facefusion-error"`)
			assert.Contains(t, rec.Body.String(), tc.reason)
			assert.Contains(t, rec.Body.String(), `value="`+tc.form.Get("python")+`"`, "what was typed stays")
			assert.NoFileExists(t, s.file)
		})
	}
}

func TestADamagedFileIsTheProblemOfTheSectionOnly(t *testing.T) {
	s := newScreen(t)
	require.NoError(t, os.WriteFile(s.file, []byte("{"), 0o600))

	assert.Contains(t, s.get(paths.FaceFusionSettings()), `data-testid="facefusion-problem"`)
	assert.Contains(t, s.get(paths.Settings()), "設定ファイルを読めません")
}
