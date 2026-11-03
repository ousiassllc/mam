package modelnodes_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/service/modelnodes"
)

// An install stopped while the engine is being checked still takes the node out and brings the engine back: the node must not vanish
// with the engine left stopped.
func TestAnInstallCancelledWhileTheNodeIsCheckedStartsTheEngineAgain(t *testing.T) {
	r := newRig(t, nil)
	r.engine.view.Process.State = proc.StateRunning
	ref, err := r.svc.Install(context.Background(), "pack", r.accept())
	require.NoError(t, err)
	r.loaded = func([]string) []string {
		require.NoError(t, r.svc.CancelInstall(ref.JobID))
		return []string{"PackApply"}
	}

	st := r.wait(ref.JobID)

	require.Equal(t, domain.JobFailed, st.State)
	assert.Empty(t, r.nodes.Enabled())
	assert.Equal(t, []string{"stop", "start", "stop", "start"}, r.engine.Calls(), "the engine is started again without the node")
	assert.Equal(t, proc.StateRunning, r.engine.Engine().Process.State)
}

// The JSON of a node (the MCP tools return it as is) is snake_case throughout: "Folder" once came out with its Go name.
func TestTheJSONOfAViewHasOnlySnakeCaseKeys(t *testing.T) {
	data, err := json.Marshal(modelnodes.View{Folder: "pack"})
	require.NoError(t, err)
	var keys map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &keys))
	assert.Contains(t, keys, "folder")
	for k := range keys {
		assert.Regexp(t, `^[a-z][a-z0-9_]*$`, k)
	}
}
