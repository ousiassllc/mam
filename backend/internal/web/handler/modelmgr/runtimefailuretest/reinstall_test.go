// Package runtimefailuretest checks what the card of a local runtime (Ollama, llama.cpp) says about a start that failed once the runtime was installed
// again (#1167). It is a package of its own because it needs the fake runtime of lrtest as the child, whose TestMain differs from that of failuretest.
package runtimefailuretest_test

import (
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime/lrtest"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
	"github.com/ousiassllc/mam/backend/internal/web/screentest"
)

func TestMain(m *testing.M) {
	if lrtest.ServeIfAsked() {
		return
	}
	gin.SetMode(gin.ReleaseMode)
	mmtest.Main(m)
}

// A runtime that was installed again is not drawn as the one that failed to start before (#1167): the install takes the error, the badge and the end
// of the log away, and a start that fails after it shows the end of its own run only (the log file keeps both runs).
func TestInstallingARuntimeAgainTakesThePreviousStartFailureOffItsCard(t *testing.T) {
	for _, id := range []string{lrtest.OllamaRuntimeID, lrtest.LLMRuntimeID} {
		t.Run(id, func(t *testing.T) {
			var run atomic.Int32
			says := func(string) string {
				if run.Load() == 0 {
					return "FIRST-RUN Error: could not load the model"
				}
				return "SECOND-RUN DLL load failed"
			}
			s := screentest.NewWithRuntimes(t, lrtest.WithLLMRuntime(), lrtest.WithOllamaRuntime(), lrtest.Saying(says))
			card := func() string { return s.Get(paths.ModelManagerPart("runtimes", id)).Body.String() }
			install := func() {
				t.Helper()
				rec := s.Do(http.MethodPost, paths.ModelManagerPart("runtimes", id, "install"), nil)
				require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
				s.WaitJob(screentest.JobID(t, rec.Body.String()))
			}
			startFails := func() {
				t.Helper()
				rec := s.Do(http.MethodPost, paths.ModelManagerPart("runtimes", id, "start"), nil)
				require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
				require.Eventually(t, func() bool {
					v, _ := s.Rts.Svc.Status(t.Context(), id)
					return v.Process.State == proc.StateFailed
				}, 20*time.Second, 20*time.Millisecond)
			}

			install()
			startFails()
			body := card()
			require.Contains(t, body, `data-testid="mm-runtime-failure"`)
			require.Contains(t, body, "FIRST-RUN")

			install() // the update
			body = card()
			assert.NotContains(t, body, `data-testid="mm-runtime-failure"`)
			assert.NotContains(t, body, "FIRST-RUN")
			v, err := s.Rts.Svc.Status(t.Context(), id)
			require.NoError(t, err)
			assert.Equal(t, proc.Status{State: proc.StateStopped}, v.Process, "no failed state, no error")
			assert.Empty(t, v.LogTail, "the log of the files that are gone is not the log of the new ones")

			run.Store(1)
			startFails()
			body = card()
			assert.Contains(t, body, "SECOND-RUN")
			assert.NotContains(t, body, "FIRST-RUN")
			v, _ = s.Rts.Svc.Status(t.Context(), id)
			assert.Contains(t, v.LogTail, "SECOND-RUN")
			assert.NotContains(t, v.LogTail, "FIRST-RUN", "the end of the log is of the run that failed now, though the file holds both")
		})
	}
}
