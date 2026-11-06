package facefusionscreen_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/facefusion"
	"github.com/ousiassllc/mam/backend/internal/media/locate"
	"github.com/ousiassllc/mam/backend/internal/service/facefusionsettings"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
	"github.com/ousiassllc/mam/backend/internal/web/screentest"
)

// TestMain starts the fake engine the model manager rig needs.
func TestMain(m *testing.M) {
	gin.SetMode(gin.ReleaseMode)
	mmtest.Main(m)
}

// screen is the page of the companion software of the model manager with the FaceFusion section over files of the test's temporary directory only.
type screen struct {
	*screentest.Screen
	t    *testing.T
	file string
}

func newScreen(t *testing.T) *screen {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the layout uses POSIX paths")
	}
	svc, file := screentest.IsolatedFaceFusion(t)
	return &screen{Screen: screentest.NewWithCompanion(t, screentest.Companion{FaceFusion: svc}), t: t, file: file}
}

// newScreenSearching is the screen whose lookup searches dir for FaceFusion and tries python first (nothing else on the machine counts): what is put in them later is
// found the next time the section is read.
func newScreenSearching(t *testing.T, dir, python string) *screen {
	t.Helper()
	file := filepath.Join(t.TempDir(), facefusionsettings.FileName)
	finder := facefusion.Finder{Finder: locate.Finder{GOOS: "linux", Path: "/nowhere", Usable: func(p string) bool {
		return strings.HasPrefix(p, os.TempDir()) && locate.IsExecutable(p)
	}}, Dirs: []string{dir}, Pythons: []string{python}}
	svc := facefusionsettings.New(file, "", facefusionsettings.WithFinder(finder))
	return &screen{Screen: screentest.NewWithCompanion(t, screentest.Companion{FaceFusion: svc}), t: t, file: file}
}

// get is the section as the page's htmx reads it (GET /model-manager/companion/facefusion: it looks again).
func (s *screen) get(path string) string {
	rec := s.Get(path)
	require.Equal(s.t, http.StatusOK, rec.Code)
	return rec.Body.String()
}

func (s *screen) put(form url.Values) *httptest.ResponseRecorder {
	return s.Do(http.MethodPut, paths.CompanionFaceFusion(), form)
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

	body := s.get(paths.CompanionFaceFusion())

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
	assert.Contains(t, s.PageOf("companion"), `id="mm-facefusion"`, "the page of the companion software has the section")
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
	assert.Contains(t, s.Page(), "FaceFusion: 使えます", "the overview of the model manager says it")
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

	assert.Contains(t, s.get(paths.CompanionFaceFusion()), `data-testid="facefusion-problem"`)
	assert.Contains(t, s.Page(), "FaceFusion: 設定ファイルを読めません", "the overview of the model manager says it")
}
