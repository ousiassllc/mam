// Package trialgonetest_test tests that the model manager has no "試し生成" any more (Issue #1100): no section, no route, no out-of-band
// draw of it in an answer, and a link in the help of the models section to the generation test screen, which is where an installed model is tried.
// It is a package of its own because the directory of the handlers is full (linterly).
package trialgonetest_test

import (
	"net/http"
	"net/url"
	"regexp"
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

// The page has no trial section, form or list: no id and no data-testid that starts with mm-trial.
func TestThePageHasNoTrialSection(t *testing.T) {
	s := screentest.New(t)
	s.Rig.InstallEngine()
	page := s.Page()

	assert.NotContains(t, page, "mm-trial", "no #mm-trial, #mm-trials, #mm-trial-form or mm-trial-* test id")
	assert.NotContains(t, page, "試し生成")
	assert.NotContains(t, page, paths.ModelManager()+"/trials")
}

// The help of the models section links to the generation test screen, once.
func TestTheHelpOfTheModelsSectionLinksToTheGenerationTest(t *testing.T) {
	s := screentest.New(t)
	page := s.Page()

	assert.Len(t, regexp.MustCompile(`data-testid="mm-open-gentest"`).FindAllString(page, -1), 1, "exactly one link")
	assert.Regexp(t, `<a[^>]*href="`+regexp.QuoteMeta(paths.GenerationTest())+`"[^>]*data-testid="mm-open-gentest"`, page)
	assert.Regexp(t, `(?s)<section[^>]*>.{0,300}<h2[^>]*>モデル</h2>.*?mm-open-gentest.*?id="mm-catalog"`, page, "in the help of the models section, before its list")
}

// The five routes of the trials are gone: nothing is registered under them.
func TestTheTrialRoutesAreNotRegistered(t *testing.T) {
	s := screentest.New(t)
	base := paths.ModelManagerPart("trials")
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, base},
		{http.MethodGet, paths.ModelManagerPart("trials", "form")},
		{http.MethodPost, base},
		{http.MethodDelete, paths.ModelManagerPart("trials", "t1")},
		{http.MethodGet, paths.ModelManagerPart("trials", "t1", "file")},
		{http.MethodGet, "/model-picker/model-trial"},
	} {
		rec := s.Do(c.method, c.path, nil)
		assert.Equal(t, http.StatusNotFound, rec.Code, c.method+" "+c.path)
	}
}

// What an install, a delete, the age gate and the commercial-use setting answer is the list (and the card of the setting): no section of a trial is drawn out of band.
func TestNoAnswerDrawsATrialSectionOutOfBand(t *testing.T) {
	s := screentest.New(t)
	s.Rig.InstallEngine()
	s.Rig.ConfirmAdult()
	require.Equal(t, "succeeded", string(s.Rig.InstallModel("m-a").State))

	answers := map[string]string{
		"models?jobs":  s.Get(paths.ModelManagerPart("models") + "?jobs=none").Body.String(),
		"adult":        s.Do(http.MethodPut, paths.ModelManagerPart("adult"), url.Values{"on": {"0"}}).Body.String(),
		"commercial":   s.Do(http.MethodPut, paths.ModelManagerPart("commercial"), url.Values{"on": {"1"}}).Body.String(),
		"model delete": s.Do(http.MethodDelete, paths.ModelManagerPart("models", "m-a"), nil).Body.String(),
		"hf models":    s.Get(paths.ModelManagerPart("hf", "models") + "?jobs=none").Body.String(),
		"hf delete":    s.Do(http.MethodDelete, paths.ModelManagerPart("hf", "models")+"?path=models/checkpoints/none.safetensors", nil).Body.String(),
	}
	for name, body := range answers {
		assert.NotContains(t, body, "mm-trial", name)
	}
}

// The generation test screen is where a model is tried now, and it is not touched.
func TestTheGenerationTestScreenStays(t *testing.T) {
	s := screentest.New(t)
	rec := s.Visit(paths.GenerationTest())
	assert.Equal(t, http.StatusOK, rec.Code)
}
