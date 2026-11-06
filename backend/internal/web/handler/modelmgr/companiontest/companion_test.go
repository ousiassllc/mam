// Package companiontest_test tests the companion software of the model manager (Issue #1106): the page "付随ソフト" (the list and the sections), the overview's line of
// it, the card of the settings overview and the old settings pages that now lead here. ffmpeg's routes and FaceFusion's instructions have their own packages of tests
// (web/ffmpegscreen, web/facefusionscreen). It is a package of its own because the directory of the handlers is full (linterly).
package companiontest_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/facefusion"
	"github.com/ousiassllc/mam/backend/internal/media/locate"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
	"github.com/ousiassllc/mam/backend/internal/web/screentest"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.ReleaseMode)
	mmtest.Main(m)
}

func count(page, id string) int { return strings.Count(page, ` id="`+id+`"`) }

// rowsOf are the rows of the list: the tool, the state and the link of each, in the order of the page.
func rowsOf(t *testing.T, page string) []map[string]string {
	t.Helper()
	var rows []map[string]string
	for _, m := range regexp.MustCompile(`<li data-testid="mm-companion-row" data-tool="([^"]+)" data-state="([^"]+)">\s*<a href="([^"]+)" data-testid="mm-companion-open" aria-label="([^"]+)"`).FindAllStringSubmatch(page, -1) {
		rows = append(rows, map[string]string{"tool": m[1], "state": m[2], "href": m[3], "label": m[4]})
	}
	return rows
}

func tools(rows []map[string]string) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r["tool"])
	}
	return out
}

// executable writes an executable file and returns its path.
func executable(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(p, []byte("#!/bin/sh\n"), 0o600))
	require.NoError(t, os.Chmod(p, 0o700)) // #nosec G302 -- the test file has to be executable
	return p
}

// faceFusionFiles writes the files of a FaceFusion installation and a python, and returns their paths.
func faceFusionFiles(t *testing.T) facefusion.Config {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "facefusion")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "facefusion"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "facefusion.py"), nil, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "facefusion", "metadata.py"), []byte("METADATA =\\\n{\n\t'version': '3.9.2',\n\t'license': 'OpenRAIL-AS',\n}\n"), 0o600))
	return facefusion.Config{Python: executable(t, "python"), Path: dir}
}

// The page is a category of its own: the list, then ffmpeg and FaceFusion, each section once. The list has a row for each software in the order of the spec, each a link to
// where it is installed, named "<name>を入れる".
func TestThePageHasTheListAndTheSectionsOnceAndTheRowsInOrder(t *testing.T) {
	s := screentest.NewAll(t)

	rec := s.Visit(paths.ModelManagerCategory("companion"))

	require.Equal(t, http.StatusOK, rec.Code)
	page := rec.Body.String()
	assert.Equal(t, 1, strings.Count(page, `data-testid="model-manager" data-category="companion"`))
	for _, id := range []string{"mm-companion-index", "mm-ffmpeg", "mm-facefusion"} {
		assert.Equal(t, 1, count(page, id), "#"+id)
	}
	assert.Contains(t, page, `<ul role="list" class="grid grid-cols-1 gap-4 sm:grid-cols-2" data-testid="mm-companion-index">`)
	rows := rowsOf(t, page)
	assert.Equal(t, []string{"ffmpeg", "facefusion", "face-models", "comfyui", "local-runtimes", "custom-nodes", "codex"}, tools(rows), "the order of the spec; ollama is #1105's")
	wantHref := map[string]string{
		"ffmpeg": "/model-manager/companion#mm-ffmpeg", "facefusion": "/model-manager/companion#mm-facefusion",
		"face-models": "/model-manager/face-check#mm-face-models", "comfyui": "/model-manager/comfyui#mm-engine",
		"local-runtimes": "/model-manager/local-runtimes#mm-runtimes", "custom-nodes": "/model-manager/custom-nodes#mm-nodes",
		"codex": "/settings/api-keys",
	}
	for _, r := range rows {
		assert.Equal(t, wantHref[r["tool"]], r["href"], r["tool"])
		assert.True(t, strings.HasSuffix(r["label"], "を入れる"), r["tool"])
		assert.NotEmpty(t, r["state"], r["tool"])
	}
	assert.Equal(t, 7, strings.Count(page, `data-testid="mm-companion-state"`), "each row says its state in words")
	assert.Regexp(t, `(?s)<h2[^>]*>付随ソフトの一覧</h2>.*<h3[^>]*>ffmpeg・ffprobe</h3>`, page, "the headings go h1, h2 of the section, h3 of a row")
	assert.Regexp(t, `(?s)<h1[^>]*>付随ソフト</h1>`, page)
	assert.Contains(t, page, `id="mm-companion-index-refresh"`, "the list has its refresh button")
}

// A row is left out when the build lacks the part; the sections too. The build with neither ffmpeg nor FaceFusion has no page.
func TestARowAndASectionAreLeftOutWhenTheBuildLacksThePart(t *testing.T) {
	ffmpeg, _ := screentest.IsolatedFFmpeg(t)
	only := screentest.NewWithCompanion(t, screentest.Companion{FFmpeg: ffmpeg})
	page := only.PageOf("companion")
	assert.Equal(t, []string{"ffmpeg", "comfyui"}, tools(rowsOf(t, page)), "no FaceFusion, no face models, runtimes, nodes or Codex here")
	assert.Equal(t, 1, count(page, "mm-ffmpeg"))
	assert.Zero(t, count(page, "mm-facefusion"))

	faceFusion, _ := screentest.IsolatedFaceFusion(t)
	only = screentest.NewWithCompanion(t, screentest.Companion{FaceFusion: faceFusion})
	page = only.PageOf("companion")
	assert.Equal(t, []string{"facefusion", "comfyui"}, tools(rowsOf(t, page)))
	assert.Zero(t, count(page, "mm-ffmpeg"))
	assert.Equal(t, 1, count(page, "mm-facefusion"))

	none := screentest.NewWithCompanion(t, screentest.Companion{})
	assert.Equal(t, http.StatusNotFound, none.Visit(paths.ModelManagerCategory("companion")).Code)
	assert.NotContains(t, none.Page(), `data-category="companion"`)
	assert.Equal(t, http.StatusNotFound, none.Get(paths.CompanionIndex()).Code)
}

// The list alone answers its refresh button, always 200, and says what it reads now.
func TestTheListAloneAnswersItsRefreshButton(t *testing.T) {
	ffmpeg, _ := screentest.IsolatedFFmpeg(t)
	s := screentest.NewWithCompanion(t, screentest.Companion{FFmpeg: ffmpeg})
	before := s.Get(paths.CompanionIndex())
	require.Equal(t, http.StatusOK, before.Code)
	assert.NotContains(t, before.Body.String(), "<html", "the list alone")
	assert.Equal(t, 1, count(before.Body.String(), "mm-companion-index"))
	assert.Contains(t, before.Body.String(), `id="mm-companion-index-refresh"`)
	assert.Contains(t, before.Body.String(), `data-tool="ffmpeg" data-state="missing"`)

	require.NoError(t, ffmpeg.Set(locate.Config{FFmpeg: executable(t, "ffmpeg"), FFprobe: executable(t, "ffprobe")}))

	assert.Contains(t, s.Get(paths.CompanionIndex()).Body.String(), `data-tool="ffmpeg" data-state="installed"`, "the list reads the state again")
}

// The line of the overview says ffmpeg and FaceFusion as they stand; FaceFusion not being set up is the usual state (partial), not a warning.
func TestTheOverviewLineOfTheCompanionSoftwareFollowsFFmpegAndFaceFusion(t *testing.T) {
	for name, tc := range map[string]struct {
		ffmpeg, faceFusion bool
		state, text        string
	}{
		"neither":         {false, false, "attention", "ffmpeg: 見つかりません・FaceFusion: 未設定"},
		"ffmpeg only":     {true, false, "partial", "ffmpeg: 使えます・FaceFusion: 未設定"},
		"both":            {true, true, "full", "ffmpeg: 使えます・FaceFusion: 使えます"},
		"FaceFusion only": {false, true, "attention", "ffmpeg: 見つかりません・FaceFusion: 使えます"},
	} {
		t.Run(name, func(t *testing.T) {
			ffmpeg, _ := screentest.IsolatedFFmpeg(t)
			faceFusion, _ := screentest.IsolatedFaceFusion(t)
			if tc.ffmpeg {
				require.NoError(t, ffmpeg.Set(locate.Config{FFmpeg: executable(t, "ffmpeg"), FFprobe: executable(t, "ffprobe")}))
			}
			if tc.faceFusion {
				require.NoError(t, faceFusion.Set(faceFusionFiles(t)))
			}
			s := screentest.NewWithCompanion(t, screentest.Companion{FFmpeg: ffmpeg, FaceFusion: faceFusion})

			page := s.Page()

			m := regexp.MustCompile(`data-category="companion" data-state="([^"]+)"`).FindStringSubmatch(page)
			require.Len(t, m, 2)
			assert.Equal(t, tc.state, m[1])
			assert.Contains(t, page, tc.text)
			assert.Regexp(t, `data-state="`+tc.state+`" data-testid="mm-category-summary"`, page)
		})
	}
}

// A settings file that cannot be read is the thing to look at (attention), for each of them.
func TestAnUnreadableSettingsFileIsAttention(t *testing.T) {
	ffmpeg, ffmpegFile := screentest.IsolatedFFmpeg(t)
	faceFusion, faceFile := screentest.IsolatedFaceFusion(t)
	s := screentest.NewWithCompanion(t, screentest.Companion{FFmpeg: ffmpeg, FaceFusion: faceFusion})
	require.NoError(t, ffmpeg.Set(locate.Config{FFmpeg: executable(t, "ffmpeg"), FFprobe: executable(t, "ffprobe")}))
	require.NoError(t, os.WriteFile(faceFile, []byte("{"), 0o600))

	page := s.Page()

	assert.Contains(t, page, `data-category="companion" data-state="attention"`)
	assert.Contains(t, page, "ffmpeg: 使えます・FaceFusion: 設定ファイルを読めません")
	assert.Regexp(t, `data-tool="facefusion" data-state="attention"`, s.PageOf("companion"))

	require.NoError(t, os.WriteFile(ffmpegFile, []byte("{"), 0o600))
	assert.Contains(t, s.Page(), "ffmpeg: 設定ファイルを読めません")
}

// The old pages of the settings lead to the companion software (303, no fragment); their writes are gone (404, not 405), and so is the download.
func TestTheOldSettingsPagesLeadHereAndTheirWritesAreGone(t *testing.T) {
	ffmpeg, _ := screentest.IsolatedFFmpeg(t)
	faceFusion, _ := screentest.IsolatedFaceFusion(t)
	s := screentest.NewWithCompanion(t, screentest.Companion{FFmpeg: ffmpeg, FaceFusion: faceFusion})

	for _, path := range []string{"/settings/ffmpeg", "/settings/facefusion"} {
		for _, htmx := range []bool{false, true} {
			rec := s.Visit(path)
			if htmx {
				rec = s.Get(path)
			}
			assert.Equal(t, http.StatusSeeOther, rec.Code, path)
			assert.Equal(t, "/model-manager/companion", rec.Header().Get("Location"), path)
		}
		for _, method := range []string{http.MethodPut, http.MethodPost, http.MethodDelete} {
			assert.Equal(t, http.StatusNotFound, s.Do(method, path, nil).Code, method+" "+path)
		}
	}
	for _, path := range []string{"/settings/ffmpeg/download", "/settings/ffmpeg/download/cancel"} {
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
			assert.Equal(t, http.StatusNotFound, s.Do(method, path, nil).Code, method+" "+path)
		}
	}
}

// Even a build with no companion software leads the old pages somewhere: the model manager's overview has no such page then, so the browser is sent there and told 404 by it.
func TestTheOldSettingsPagesRedirectWhateverTheBuild(t *testing.T) {
	s := screentest.NewWithCompanion(t, screentest.Companion{})
	rec := s.Visit("/settings/ffmpeg")
	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, http.StatusNotFound, s.Visit(rec.Header().Get("Location")).Code)
}

// The settings overview has no card for ffmpeg and FaceFusion any more and has one for the companion software, which leads to its page and says the same as the overview of the model manager.
func TestTheSettingsOverviewHasOneCardForTheCompanionSoftware(t *testing.T) {
	ffmpeg, _ := screentest.IsolatedFFmpeg(t)
	faceFusion, _ := screentest.IsolatedFaceFusion(t)
	s := screentest.NewWithCompanion(t, screentest.Companion{FFmpeg: ffmpeg, FaceFusion: faceFusion})

	req := httptest.NewRequest(http.MethodGet, paths.Settings(), nil)
	rec := s.Send(req)
	require.Equal(t, http.StatusOK, rec.Code)
	page := rec.Body.String()

	assert.Equal(t, 1, strings.Count(page, `href="/model-manager/companion"`), "one card, to the page of the companion software")
	assert.Regexp(t, `<a id="tool-companion" href="/model-manager/companion"`, page)
	assert.Contains(t, page, "付随ソフト")
	assert.Contains(t, page, `data-state="attention"`)
	assert.Contains(t, page, "ffmpeg: 見つかりません・FaceFusion: 未設定")
	assert.NotContains(t, page, `href="/settings/ffmpeg"`, "no card and no menu item")
	assert.NotContains(t, page, `href="/settings/facefusion"`)
	assert.NotContains(t, page, `id="tool-ffmpeg"`)
	assert.NotContains(t, page, `id="tool-facefusion"`)
}

// The two services decide whether the card is there at all.
func TestTheSettingsOverviewHasNoCardWithoutTheCompanionSoftware(t *testing.T) {
	s := screentest.NewWithCompanion(t, screentest.Companion{})

	rec := s.Send(httptest.NewRequest(http.MethodGet, paths.Settings(), nil))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, rec.Body.String(), `id="tool-companion"`)
}
