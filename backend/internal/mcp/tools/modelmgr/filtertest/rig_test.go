// Package filtertest runs the filter of the model catalog (Issue #1011: kinds, inputs, adult, uncensored, parameter bands, installed, search...) through
// the MCP tool list_model_catalog.
package filtertest_test

import (
	"testing"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/jobs"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/modelmgr"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
)

func newEnv(t *testing.T, opts ...mmtest.Option) (*mcptest.Env, *mmtest.Rig) {
	t.Helper()
	var rig *mmtest.Rig
	env := mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) {
		rig = mmtest.New(t, append(opts, mmtest.WithDB(env.DB), mmtest.WithLogger(env.Logger))...)
		env.Services.ModelManager = rig.Svc
		jobs.Register(r, env.Services)
		modelmgr.Register(r, env.Services)
	})
	return env, rig
}
