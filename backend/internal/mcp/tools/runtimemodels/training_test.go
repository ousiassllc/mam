package runtimemodels_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/modelmgr"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime/lrtest"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
)

// A fine-tuning of the voice holds the runtime: the tools that install, start or remove it answer a conflict with the reason (not the raw error).
func TestRuntimeTools_ARuntimeThatATrainingHoldsIsRefusedAsAConflictWithTheReason(t *testing.T) {
	var mm *mmtest.Rig
	var lr *lrtest.Rig
	env := mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) {
		mm = mmtest.New(t, mmtest.WithDB(env.DB), mmtest.WithLogger(env.Logger))
		lr = lrtest.New(t, mm.Queue, mm.Table, mm.Svc.HandleInstall)
		env.Services.ModelManager, env.Services.LocalRuntimes = mm.Svc, lr.Svc
		modelmgr.Register(r, env.Services)
	})
	ref, err := lr.Svc.Install(t.Context(), lrtest.ID)
	require.NoError(t, err)
	mm.Start()
	require.Equal(t, domain.JobSucceeded, lr.Wait(ref.JobID).State)
	tool, err := lr.Svc.BeginTool(t.Context(), lrtest.ID)
	require.NoError(t, err)
	defer tool.End()

	for _, name := range []string{"install_local_runtime", "install_local_runtime_weights", "start_local_runtime", "remove_local_runtime"} {
		msg := env.CallError(name, map[string]any{"runtime_id": lrtest.ID})
		assert.Contains(t, msg, "conflict", name)
		assert.Contains(t, msg, "声の学習が動いています", name)
	}
}
