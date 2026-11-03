// Package settingstest runs the detailed settings of the generation test (Issue #973: steps, cfg, sampler, scheduler, seed) and the prompt
// weights (Issue #972) through the MCP tools.
package settingstest_test

import (
	"testing"
	"time"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	mcpgentest "github.com/ousiassllc/mam/backend/internal/mcp/tools/gentest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/jobs"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
)

type rig struct {
	*mcptest.Env
	GT *gentesttest.Rig
}

func newRig(t *testing.T, opts ...gentesttest.Option) *rig {
	t.Helper()
	r := &rig{}
	r.Env = mcptest.New(t, func(env *mcptest.Env, reg *mcpkit.Registrar) {
		r.GT = gentesttest.NewOn(t, env.DB, opts...)
		env.Services.Gentest = r.GT.Svc
		jobs.Register(reg, env.Services)
		mcpgentest.Register(reg, env.Services)
	})
	return r
}

func (r *rig) job(id string) mcpkit.JobStatus {
	r.T.Helper()
	return r.WaitJob(id, 20*time.Second, 10*time.Millisecond)
}

// run estimates the batch and runs it with the figures it was shown.
func (r *rig) run(args map[string]any) (mcpgentest.RunOutput, gentest.Estimate) {
	r.T.Helper()
	var est gentest.Estimate
	r.CallOK("estimate_generation_test", args, &est)
	run := map[string]any{"estimate_id": est.ID, "confirmed_total_usd": est.TotalUSD}
	for k, v := range args {
		run[k] = v
	}
	var out mcpgentest.RunOutput
	r.CallOK("run_generation_test", run, &out)
	return out, est
}
