package localmodels_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/configcheck"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localbackends"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/localmodels"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/settings"
	localsvc "github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
)

// rig is the local models service over a models.json in a temporary directory and a Model Adapter that a
// save rebuilds, as the app wires it.
type rig struct {
	svc     *localsvc.Service
	models  *modelsettings.Service
	adapter *modeladapter.Adapter
	keys    *credentialtest.Memory
	file    string
}

func newRig(t *testing.T, pinned bool, content string) *rig {
	t.Helper()
	file := filepath.Join(t.TempDir(), "models.json")
	existing := ""
	if content != "" {
		require.NoError(t, os.WriteFile(file, []byte(content), 0o600))
		existing = file
	}
	keys := credentialtest.New()
	cfg, err := configcheck.Load(existing)
	require.NoError(t, err)
	opts := localbackends.Options{StagingDir: t.TempDir()}
	adapter := modeladapter.New(cfg, localbackends.Build(cfg, keys, opts))
	localbackends.Publish(cfg)
	t.Cleanup(func() { localbackends.Publish(modeladapter.DefaultConfig()) })
	svc := localsvc.New(localsvc.Deps{Path: file, Pinned: pinned, Keys: keys, Adapter: adapter, Backends: opts})
	models := modelsettings.New(file, pinned)
	models.SetOnChange(func() { require.NoError(t, svc.Reload()) })
	return &rig{svc: svc, models: models, adapter: adapter, keys: keys, file: file}
}

// newEnv serves the local model tools and the model assignment tools over the rig.
func newEnv(t *testing.T, r *rig) *mcptest.Env {
	t.Helper()
	return mcptest.New(t, func(env *mcptest.Env, reg *mcpkit.Registrar) {
		env.Services.LocalModels = r.svc
		reg.Settings = mcpkit.Settings{Models: r.models}
		localmodels.Register(reg, env.Services)
		settings.Register(reg, env.Services)
	})
}

// fakeServer answers like a ComfyUI (GET /system_stats) or, with compat, like an OpenAI-compatible server.
func fakeServer(t *testing.T, compat bool) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case !compat && req.URL.Path == "/system_stats":
			_, _ = w.Write([]byte(`{"system":{"comfyui_version":"0.3.60"},"devices":[{"name":"cuda:0 RTX 4090"}]}`))
		case compat && req.URL.Path == "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"flux-schnell"}]}`))
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}
