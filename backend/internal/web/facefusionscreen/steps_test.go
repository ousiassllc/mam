package facefusionscreen_test

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/facefusion"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// stepsOf is the block of the instructions in the section.
func stepsOf(t *testing.T, body string) string {
	t.Helper()
	m := regexp.MustCompile(`(?s)<details[^>]*data-testid="mm-facefusion-steps"[^>]*>.*?</details>`).FindString(body)
	require.NotEmpty(t, m, "the instructions are in the section")
	return m
}

func isOpen(steps string) bool {
	return regexp.MustCompile(`^<details[^>]*\sopen[\s>]`).MatchString(steps)
}

// While FaceFusion is not found the instructions are open: the five steps of the official guide, each command in a block of its own, pinned to the tag mam checked.
func TestSteps_AreOpenWhileFaceFusionIsNotFoundAndHoldTheFiveStepsOfTheOfficialGuide(t *testing.T) {
	s := newScreen(t)

	steps := stepsOf(t, s.get(paths.CompanionFaceFusion()))

	assert.True(t, isOpen(steps), "open from the start while there is no FaceFusion")
	assert.Equal(t, 5, strings.Count(steps, "<li"), "five numbered steps")
	assert.Equal(t, 1, strings.Count(steps, "<ol"), "an ordered list")
	assert.Contains(t, steps, "mam は FaceFusion を取得しません")
	for _, cmd := range []string{
		"conda init --all", "conda create --name facefusion python=3.12 pip=25.0", "conda activate facefusion",
		"git clone https://github.com/facefusion/facefusion", "cd facefusion", "git checkout 3.9.2",
		"python install.py cuda@12", "python install.py default", "python install.py directml", "conda deactivate",
	} {
		assert.Contains(t, steps, "<code>"+cmd+"</code>", cmd)
	}
	assert.Equal(t, "3.9.2", facefusion.RecommendedTag, "the tag the steps check out")
	assert.NotContains(t, steps, "v4-beta")
	assert.Equal(t, 11, strings.Count(steps, `data-testid="mm-facefusion-cmd"`), "one block for each command")
	blocks := regexp.MustCompile(`<pre[^>]*data-testid="mm-facefusion-cmd"[^>]*>`).FindAllString(steps, -1)
	require.Len(t, blocks, 11)
	for _, b := range blocks {
		assert.Contains(t, b, `tabindex="0"`, "the keyboard reaches the block")
		assert.Contains(t, b, `aria-label="コマンド: `, "and the reader names it")
	}
}

// Once FaceFusion is found the instructions are closed (still there, to read again).
func TestSteps_AreClosedOnceFaceFusionIsFound(t *testing.T) {
	s := newScreen(t)
	dir, python := install(t)
	require.Equal(t, http.StatusOK, s.put(url.Values{"python": {python}, "path": {dir}}).Code)

	body := s.get(paths.CompanionFaceFusion())

	steps := stepsOf(t, body)
	assert.False(t, isOpen(steps), "closed once there is FaceFusion")
	assert.Contains(t, steps, "git checkout 3.9.2", "but still there")
}

// mam does not fetch FaceFusion: there is no button, no route and no job that installs it. It is the decision of spec/architecture/gentest/face-swap.md §1.
func TestFaceFusionIsNeverFetched_NoInstallButtonNoInstallRouteNoJob(t *testing.T) {
	s := newScreen(t)

	body := s.PageOf("companion")
	section := regexp.MustCompile(`(?s)<div id="mm-facefusion">.*`).FindString(body)
	require.NotEmpty(t, section)

	assert.NotContains(t, section, "hx-post", "nothing in the section posts: nothing starts an install")
	assert.NotContains(t, section, "hx-delete", "nothing removes what mam never put there")
	assert.NotRegexp(t, `(?i)ダウンロードして入れる|導入する|インストールする|data-job=`, section)
	assert.Regexp(t, `hx-get="/model-manager/companion/facefusion"`, section, "the one read is the search again")
	for _, route := range []string{
		paths.CompanionFaceFusion(), paths.CompanionFaceFusion() + "/install", paths.CompanionFaceFusion() + "/pull",
		paths.CompanionFaceFusion() + "/download", paths.CompanionFaceFusion() + "/download/cancel",
	} {
		want := http.StatusNotFound
		if route == paths.CompanionFaceFusion() {
			want = http.StatusMethodNotAllowed // the route is there for GET and PUT only
		}
		for _, method := range []string{http.MethodPost, http.MethodDelete} {
			assert.Equal(t, want, s.Do(method, route, nil).Code, method+" "+route)
		}
	}
	assert.Equal(t, http.StatusNotFound, s.Get(paths.CompanionFaceFusion()+"/install").Code)
}

// A version that is not 3.x gets one line (the models of mam are the choices of 3.x); a 3.x one gets none. Nothing is refused.
func TestVersionNote_IsSaidOnlyForAVersionThatIsNotThreeDotX(t *testing.T) {
	s := newScreen(t)
	dir, python := install(t)
	require.Equal(t, http.StatusOK, s.put(url.Values{"python": {python}, "path": {dir}}).Code)

	assert.NotContains(t, s.get(paths.CompanionFaceFusion()), `data-testid="mm-facefusion-version-note"`, "3.9.2 is the one mam checked")

	meta := filepath.Join(dir, "facefusion", "metadata.py")
	require.NoError(t, os.WriteFile(meta, []byte("METADATA =\\\n{\n\t'version': '4.0.0',\n\t'license': 'OpenRAIL-AS',\n}\n"), 0o600))

	body := s.get(paths.CompanionFaceFusion())

	assert.Contains(t, body, `data-testid="mm-facefusion-version-note"`)
	assert.Contains(t, body, "mam が確かめたのは 3.x の選択肢（モデルの名前）です。この版では入れ替えが動かないことがあります")
	assert.Contains(t, body, "FaceFusion 4.0.0", "it is still found and usable")
	assert.Contains(t, body, "使えます")
}

// "もう一度探す" looks again without saving anything and without running FaceFusion: the state changes once the user has installed it.
func TestFindAgain_LooksAgainSavesNothingAndRunsNothing(t *testing.T) {
	root := t.TempDir()
	dir, python := filepath.Join(root, "facefusion"), filepath.Join(root, "python")
	s := newScreenSearching(t, dir, python)

	page := s.PageOf("companion")
	assert.Regexp(t, `<button[^>]*data-testid="mm-facefusion-refind"`, page)
	assert.Regexp(t, `hx-get="/model-manager/companion/facefusion"[^>]*hx-target="#facefusion-panel"|hx-target="#facefusion-panel"[^>]*hx-get="/model-manager/companion/facefusion"`, page)
	before := s.get(paths.CompanionFaceFusion())
	assert.Contains(t, before, "未設定")
	assert.Contains(t, before, `role="status" data-testid="facefusion-state"`, "the result is read out")

	// The user installs FaceFusion (here: the files of an installation, and a python that would leave a mark if it were run).
	marker := filepath.Join(root, "was-run")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "facefusion"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "facefusion.py"), nil, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "facefusion", "metadata.py"), []byte("METADATA =\\\n{\n\t'version': '3.9.2',\n\t'license': 'OpenRAIL-AS',\n}\n"), 0o600))
	require.NoError(t, os.WriteFile(python, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o600))
	require.NoError(t, os.Chmod(python, 0o700)) // #nosec G302 -- the test file has to be executable

	after := s.get(paths.CompanionFaceFusion())

	assert.Contains(t, after, "使えます")
	assert.Contains(t, after, "FaceFusion 3.9.2（OpenRAIL-AS）")
	assert.False(t, isOpen(stepsOf(t, after)), "the instructions close")
	assert.NoFileExists(t, s.file, "nothing is saved")
	assert.NoFileExists(t, marker, "FaceFusion is not run: only its files are read")
}

// The section is also the answer of a plain GET (a browser that is not htmx gets the page).
func TestTheSectionOfANavigationIsThePage(t *testing.T) {
	s := newScreen(t)

	rec := s.Visit(paths.CompanionFaceFusion())

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "<html")
	assert.Contains(t, rec.Body.String(), `data-category="companion"`)
}
