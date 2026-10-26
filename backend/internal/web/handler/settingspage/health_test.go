package settingspage_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/configcheck"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localbackends"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/router/settingsroutes"
	localsvc "github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/web/handler/settingspage"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// localService is a local models service over a models.json in a temporary directory, with the endpoints of base URLs
// (name → URL).
func localService(t *testing.T, endpoints map[string]string) *localsvc.Service {
	t.Helper()
	file := filepath.Join(t.TempDir(), "models.json")
	keys := credentialtest.New()
	cfg, err := configcheck.Load("")
	require.NoError(t, err)
	opts := localbackends.Options{StagingDir: t.TempDir()}
	adapter := modeladapter.New(cfg, localbackends.Build(cfg, keys, opts))
	local := localsvc.New(localsvc.Deps{Path: file, Keys: keys, Adapter: adapter, Backends: opts})
	for name, url := range endpoints {
		require.NoError(t, local.CreateEndpoint(context.Background(), localsvc.EndpointInput{Name: name, Kind: "comfyui", BaseURL: url}))
	}
	return local
}

func localScreen(t *testing.T, endpoints map[string]string) *screen {
	t.Helper()
	local := localService(t, endpoints)
	return newScreen(t, func(s *settingsroutes.Services) { s.LocalModels = local })
}

// The overview card of the local models says how many endpoints answer, read after the page is shown: the page itself
// only counts what is registered, so that an endpoint that does not answer (ten seconds to find out) never delays it.
func TestHealth_SaysHowManyEndpointsAnswerAndTheOverviewCardReadsIt(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/system_stats" {
			_, _ = w.Write([]byte(`{"system":{"comfyui_version":"0.3"},"devices":[]}`))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(up.Close)
	down := httptest.NewServer(http.NotFoundHandler())
	downURL := down.URL
	down.Close()
	s := localScreen(t, map[string]string{"up": up.URL, "down": downURL})

	overview := s.get(paths.Settings()).Body.String()
	assert.Contains(t, overview, "接続先 2 件・モデル 0 件")
	assert.Contains(t, overview, `hx-get="/settings/local-models/health"`)
	assert.NotContains(t, overview, "件に接続できます", "the connection is not tried while the page is drawn")

	rec := s.get(paths.LocalModelsHealth())
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "2 件中 1 件に接続できます")
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
}

func TestHealth_NoEndpointsHasNoLineAndNoRead(t *testing.T) {
	s := localScreen(t, nil)

	overview := s.get(paths.Settings()).Body.String()
	assert.Contains(t, overview, "接続先なし")
	assert.NotContains(t, overview, paths.LocalModelsHealth())
	rec := s.get(paths.LocalModelsHealth())
	assert.Equal(t, http.StatusOK, rec.Code, "never an error toast on a card that is only a link")
	assert.Empty(t, rec.Body.String())
}

// An endpoint that answers slower than the deadline counts as not connected, and the answer comes at the deadline.
func TestHealth_AnEndpointThatDoesNotAnswerInTimeIsNotConnected(t *testing.T) {
	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	t.Cleanup(func() { close(release); slow.Close() })
	local := localService(t, map[string]string{"slow": slow.URL})
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET(paths.LocalModelsHealth(), settingspage.Health(local, 50*time.Millisecond))

	rec := httptest.NewRecorder()
	start := time.Now()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, paths.LocalModelsHealth(), nil))

	assert.Contains(t, rec.Body.String(), "1 件中 0 件に接続できます")
	assert.Less(t, time.Since(start), 5*time.Second, "the answer does not wait for the endpoint")
}
