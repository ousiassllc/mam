package modelmgr_test

import (
	"testing"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/jobs"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/modelmgr"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime/lrtest"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
)

func TestMain(m *testing.M) {
	if lrtest.ServeIfAsked() {
		return
	}
	mmtest.Main(m)
}

// newEnv is the MCP environment with the model manager over fakes (a fake Hugging Face, a fake engine process) on the
// database of the environment, so that get_job reads the job table the manager's jobs are in.
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

// newRuntimeEnv is newEnv with the local runtimes over fakes (a fake package and weights; a Windows PC with an RTX 4070 unless an option
// says another), on the queue and database of the model manager.
func newRuntimeEnv(t *testing.T, opts ...lrtest.Option) (*mcptest.Env, *mmtest.Rig, *lrtest.Rig) {
	t.Helper()
	var mm *mmtest.Rig
	var lr *lrtest.Rig
	env := mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) {
		mm = mmtest.New(t, mmtest.WithDB(env.DB), mmtest.WithLogger(env.Logger))
		lr = lrtest.New(t, mm.Queue, mm.Table, mm.Svc.HandleInstall, opts...)
		env.Services.ModelManager, env.Services.LocalRuntimes = mm.Svc, lr.Svc
		jobs.Register(r, env.Services)
		modelmgr.Register(r, env.Services)
	})
	return env, mm, lr
}
