package failuretest_test

import (
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
	"github.com/ousiassllc/mam/backend/internal/web/screentest"
)

// An engine that was installed again is not drawn as the one that failed to start before (#1167): the install takes the badge, the error and the
// end of the log away, and a start that fails after it shows the end of its own run only.
func TestInstallingTheEngineAgainTakesThePreviousStartFailureOffTheCard(t *testing.T) {
	var run atomic.Int32
	says := func() string {
		if run.Load() == 0 {
			return "FIRST-RUN can't open file main.py: No such file or directory"
		}
		return "SECOND-RUN DLL load failed"
	}
	s := screentest.New(t, mmtest.WithEngineSaying(says))
	s.Rig.InstallEngine()
	startFails := func() string {
		t.Helper()
		rec := s.Do(http.MethodPost, paths.ModelManagerPart("engine", "start"), nil)
		require.Equal(t, http.StatusAccepted, rec.Code)
		require.Eventually(t, func() bool { return s.Rig.Svc.Engine().Process.State == proc.StateFailed }, 20*time.Second, 20*time.Millisecond)
		return s.Get(paths.ModelManagerPart("engine")).Body.String()
	}

	body := startFails()
	require.Contains(t, body, "起動に失敗")
	require.Contains(t, body, "FIRST-RUN")

	rec := s.Do(http.MethodPost, paths.ModelManagerPart("engine", "install"), url.Values{"variant": {"nvidia"}})
	require.Equal(t, http.StatusAccepted, rec.Code)
	job := screentest.JobID(t, rec.Body.String())
	s.WaitJob(job)
	body = s.Get(paths.ModelManagerPart("engine") + "?job=" + job).Body.String()
	assert.NotContains(t, body, "起動に失敗")
	assert.NotContains(t, body, `data-testid="mm-engine-failure"`)
	assert.NotContains(t, body, "FIRST-RUN")
	assert.Contains(t, body, "停止中", "installed and not started yet")

	run.Store(1)
	body = startFails()
	assert.Contains(t, body, "起動に失敗")
	assert.Contains(t, body, "SECOND-RUN")
	assert.NotContains(t, body, "FIRST-RUN", "the end of the log is of the run that failed now")
}
