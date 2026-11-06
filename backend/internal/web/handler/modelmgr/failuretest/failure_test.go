// Package failuretest_test tests how the model manager screen tells an install that ended in failure (#1010): the error box of the common
// error (the cause and the remedy first, the detail folded) under a title that names what failed, not one line of the job's text. It is a
// package of its own because the directory of the handlers is full (linterly).
package failuretest_test

import (
	"net/http"
	"net/url"
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

// An engine install the user stopped: the job is failed with the common error install_cancelled, and the engine card draws the error box
// under the title, with the way back (the model manager) and no bare line.
func TestAnEngineInstallTheUserStoppedShowsTheErrorBox(t *testing.T) {
	s := screentest.New(t)
	hold := s.Rig.Host.Hold("/engine.7z")
	t.Cleanup(hold.Release)
	rec := s.Do(http.MethodPost, paths.ModelManagerPart("engine", "install"), url.Values{"variant": {"nvidia"}})
	require.Equal(t, http.StatusAccepted, rec.Code)
	job := screentest.JobID(t, rec.Body.String())
	s.Rig.Start()
	hold.Reached(t)
	cancel := s.Do(http.MethodPost, paths.ModelManagerPart("jobs", job, "cancel"), nil)
	require.Equal(t, http.StatusOK, cancel.Code, cancel.Body.String())
	hold.Release()
	s.WaitJob(job)

	body := s.Get(paths.ModelManagerPart("engine") + "?job=" + job).Body.String()

	assert.Contains(t, body, `data-testid="mm-engine-failure"`)
	assert.Contains(t, body, "ComfyUI エンジンの導入に失敗しました")
	assert.Contains(t, body, `data-testid="error-box" data-error-code="install_cancelled"`)
	assert.Contains(t, body, "導入を中断しました")
	assert.Contains(t, body, "もう一度導入すると、ダウンロード済みの続きから再開します")
	assert.Contains(t, body, `href="/model-manager"`)
	assert.Contains(t, body, `data-testid="error-copy"`)
	assert.NotContains(t, body, `data-testid="mm-engine-error"`, "the box replaces the bare line")
}

// A model install the user stopped is told in the model list the same way: the box under the title of the model.
func TestAModelInstallTheUserStoppedShowsTheErrorBoxInTheCatalog(t *testing.T) {
	s := screentest.New(t)
	s.Rig.InstallEngine()
	hold := s.Rig.Host.Hold("/" + mmtest.CkptA.Folder + "/" + mmtest.CkptA.Name)
	t.Cleanup(hold.Release)
	rec := s.Do(http.MethodPost, paths.ModelManagerPart("models", "m-a", "install"), nil)
	require.Equal(t, http.StatusAccepted, rec.Code)
	job := screentest.JobID(t, rec.Body.String())
	s.Rig.Start()
	hold.Reached(t)
	require.Equal(t, http.StatusOK, s.Do(http.MethodPost, paths.ModelManagerPart("jobs", job, "cancel"), nil).Code)
	hold.Release()
	s.WaitJob(job)

	body := s.Get(paths.ModelManagerPart("models") + "?jobs=" + job).Body.String()

	assert.Contains(t, body, `data-testid="mm-catalog-failure"`)
	assert.Contains(t, body, `data-testid="error-box" data-error-code="install_cancelled"`)
	assert.NotContains(t, body, `data-testid="mm-catalog-error"`)
}
