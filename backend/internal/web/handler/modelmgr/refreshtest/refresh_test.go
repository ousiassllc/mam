// Package refreshtest tests the refresh buttons of the model manager (#962) and the modal of the engine card (#970). It is a package of its own
// because the directory of the handlers is full (linterly).
package refreshtest_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
	"github.com/ousiassllc/mam/backend/internal/web/screentest"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.ReleaseMode)
	mmtest.Main(m)
}

// refreshOf is the request the refresh button of the part with the id makes (#962): the button swaps that part by what its url answers.
func refreshOf(t *testing.T, body, target string) string {
	t.Helper()
	m := regexp.MustCompile(`<button[^>]*id="` + target + `-refresh"[^>]*hx-get="([^"]+)"[^>]*hx-target="#` + target + `"[^>]*hx-swap="outerHTML"`).FindStringSubmatch(body)
	require.Len(t, m, 2, "no refresh button for %s in %s", target, body)
	return strings.ReplaceAll(m[1], "&amp;", "&")
}

// The engine, the model list, the files added from Hugging Face and the trial results have a refresh button that reads just its own part again, with when it was read.
// What the reader types (the trial form) and the modal that is open are outside of the parts the button swaps.
func TestRefresh_EveryPartThatChangesByItselfHasAButtonThatReadsItAgain(t *testing.T) {
	s := screentest.New(t)
	s.Rig.InstallEngine()
	page := s.Page()
	for _, target := range []string{"mm-engine", "mm-catalog", "mm-trials", "mm-hfi-models"} {
		url := refreshOf(t, page, target)
		rec := s.Get(url)
		require.Equal(t, http.StatusOK, rec.Code, target)
		body := rec.Body.String()
		assert.Contains(t, body, `id="`+target+`"`, "the answer replaces the part with the same id")
		assert.Contains(t, body, `id="`+target+`-refresh"`, "and brings the button back")
		assert.Regexp(t, `最終更新 <time>\d\d:\d\d:\d\d</time>`, body)
		assert.NotContains(t, body, "<dialog", "the modal that is open is not in an answer")
		assert.NotContains(t, body, `id="mm-trial-form"`, "nor the form that is being typed in")
	}
}

// A refresh of the engine while an install runs keeps the job it reports on, so that the card still says how the install ended.
func TestRefresh_TheEngineCardOfARunningInstallKeepsItsJob(t *testing.T) {
	s := screentest.New(t)
	rec := s.Do(http.MethodPost, paths.ModelManagerPart("engine", "install"), url.Values{"variant": {"nvidia"}})
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	job := screentest.JobID(t, rec.Body.String())
	assert.Equal(t, paths.ModelManagerPart("engine")+"?job="+job, refreshOf(t, rec.Body.String(), "mm-engine"))
	s.WaitJob(job)
}

// The button of the engine is on the card of an engine in every state that has buttons; one that cannot run has nothing to refresh but still
// answers a refresh. The card has no button named 更新 twice: the update of the engine is "更新する".
func TestRefresh_TheEngineCardHasOneRefreshButton(t *testing.T) {
	s := screentest.New(t)
	assert.Equal(t, 0, strings.Count(s.Get(paths.ModelManagerPart("engine")).Body.String(), `id="mm-engine-refresh"`), "an engine that is not installed shows the form of the install")
	s.Rig.InstallEngine()
	assert.Equal(t, 1, strings.Count(s.Get(paths.ModelManagerPart("engine")).Body.String(), `id="mm-engine-refresh"`))
}

// The modal of the engine says what the card does not: the version, the place, the license and, once running, the address.
func TestEngineDetail_SaysWhatTheCardLeavesOut(t *testing.T) {
	s := screentest.New(t)
	missing := s.Get(paths.ModelManagerPart("engine", "detail"))
	require.Equal(t, http.StatusOK, missing.Code)
	assert.Contains(t, missing.Body.String(), `data-engine="missing"`)
	assert.Contains(t, missing.Body.String(), "GPL-3.0")
	assert.Contains(t, missing.Body.String(), "data-modal-close")

	s.Rig.InstallEngine()
	installed := s.Get(paths.ModelManagerPart("engine", "detail")).Body.String()
	assert.Contains(t, installed, `data-engine="installed"`)
	assert.Contains(t, installed, `data-testid="mm-engine-dir"`)
	assert.NotContains(t, installed, `data-testid="mm-engine-url"`, "no address while it is stopped")
	assert.Contains(t, installed, `data-testid="mm-engine-facts"`)
}

// The card itself carries none of the long text: the license paragraph, the install place and the unsupported OS explanation are in the modal.
func TestEngineCard_CarriesNoLongText(t *testing.T) {
	s := screentest.New(t)
	s.Rig.InstallEngine()
	card := s.Get(paths.ModelManagerPart("engine")).Body.String()
	for _, text := range []string{"GPL-3.0", "アンインストールするとここも一緒に消えます", "SHA-256", "導入先"} {
		assert.NotContains(t, card, text)
	}
	assert.Contains(t, card, `data-card-open`)
	assert.Contains(t, card, `hx-get="`+paths.ModelManagerPart("engine", "detail")+`"`)
}
