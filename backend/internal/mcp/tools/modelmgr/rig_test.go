package modelmgr_test

import (
	"testing"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/jobs"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/modelmgr"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
)

func TestMain(m *testing.M) { mmtest.Main(m) }

// newEnv is the MCP environment with the model manager over fakes (a fake Hugging Face, a fake engine process) on the
// database of the environment, so that get_job reads the job table the manager's jobs are in.
func newEnv(t *testing.T, opts ...mmtest.Option) (*mcptest.Env, *mmtest.Rig) {
	t.Helper()
	var rig *mmtest.Rig
	env := mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) {
		rig = mmtest.New(t, append(opts, mmtest.WithDB(env.DB))...)
		env.Services.ModelManager = rig.Svc
		jobs.Register(r, env.Services)
		modelmgr.Register(r, env.Services)
	})
	return env, rig
}
