package shotgen_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/shotgen"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/shottest"
)

func TestGenerateShotCandidates_ReturnsTheJobRowIDAndFinishes(t *testing.T) {
	f := newFixture(t, shottest.Options{})
	var out shotgen.ShotJobOutput
	f.CallOK("generate_shot_candidates", map[string]any{"shot_id": f.ShotID}, &out)

	// The job_id is the id of the persisted job row: it survives a restart and is a job of kind "generate".
	assert.Equal(t, 1, f.Count(t, `SELECT COUNT(*) FROM job WHERE id = ? AND kind = 'generate'`, out.JobID))
	st := waitJob(t, f, out.JobID)
	assert.Equal(t, mcpkit.JobSucceeded, st.State, st.Error)
	assert.NotEmpty(t, st.Result["take_ids"])
}

func TestBuildReferencePack_JobSucceedsWithTheReferenceCount(t *testing.T) {
	f := newFixture(t, shottest.Options{})
	assert.Contains(t, f.CallError("build_reference_pack", map[string]any{"shot_id": "missing"}), "not_found")

	var out shotgen.ShotJobOutput
	f.CallOK("build_reference_pack", map[string]any{"shot_id": f.ShotID}, &out)
	st := waitJob(t, f, out.JobID)
	assert.Equal(t, mcpkit.JobSucceeded, st.State, st.Error)
	assert.NotZero(t, st.Result["reference_count"])
	f.WaitIdle(t)
}

func TestContinue_ProposeThenConfirmAddsTheNextShotAndGeneratesIt(t *testing.T) {
	f := newFixture(t, shottest.Options{})
	assert.Contains(t, f.CallError("propose_continue", map[string]any{"shot_id": f.ShotID}), "conflict", "nothing has been completed to continue from")
	assert.Contains(t, f.CallError("propose_continue", map[string]any{"shot_id": "missing"}), "not_found")
	f.Generate(t)

	var card shotgen.ProposeOutput
	f.CallOK("propose_continue", map[string]any{"shot_id": f.ShotID}, &card)
	assert.NotEmpty(t, card.MessageID)
	require.Len(t, card.Options, 3)

	assert.Contains(t, f.CallError("confirm_continue", map[string]any{"shot_id": f.ShotID}), "validation", "neither an instruction nor delegate")

	var out shotgen.ConfirmOutput
	f.CallOK("confirm_continue", map[string]any{"shot_id": f.ShotID, "instruction": "背後から声が聞こえる"}, &out)
	assert.NotEqual(t, f.ShotID, out.Shot.ID, "a new Shot")
	assert.Equal(t, "ドアを開けて廊下を見る", out.Shot.Objective)
	st := waitJob(t, f, out.JobID)
	assert.Equal(t, out.Shot.ID, st.Target, "the job follows the new Shot, not the one it continues from")
	assert.Equal(t, mcpkit.JobSucceeded, st.State, st.Error)
	assert.NotEmpty(t, st.Result["take_ids"])
}
