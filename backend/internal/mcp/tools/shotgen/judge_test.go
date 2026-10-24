package shotgen_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/shotgen"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/shottest"
)

func TestJudgeTake_JobSucceedsWithTheVerdictAndGetTakeJudgeResultReturnsIt(t *testing.T) {
	f := newFixture(t, shottest.Options{Verdict: shottest.FailVerdict})
	takes := f.Generate(t)
	take := takes[0]

	assert.Contains(t, f.CallError("get_take_judge_result", map[string]any{"take_id": "missing"}), "not_found")

	var out shotgen.TakeJobOutput
	f.CallOK("judge_take", map[string]any{"take_id": take}, &out)
	require.NotEmpty(t, out.JobID)
	assert.Equal(t, take, out.TakeID)

	st := waitJob(t, f, out.JobID)
	assert.Equal(t, mcpkit.JobSucceeded, st.State, st.Error)
	assert.Equal(t, "fail", st.Result["verdict"])

	var res shotgen.JudgeResultOutput
	f.CallOK("get_take_judge_result", map[string]any{"take_id": take}, &res)
	assert.Equal(t, "fail", string(res.Result.Result))
	require.NotEmpty(t, res.Result.Issues)
	assert.Equal(t, "camera_speed", res.Result.Issues[0].Type)
}

func TestJudgeTake_ErrorsMapToToolErrors(t *testing.T) {
	f := newFixture(t, shottest.Options{})
	assert.Contains(t, f.CallError("judge_take", map[string]any{"take_id": "missing"}), "not_found")

	// A Take without a video cannot be judged.
	takes := f.Generate(t)
	f.Exec(t, `UPDATE take SET file_path = '' WHERE id = ?`, takes[0])
	assert.Contains(t, f.CallError("judge_take", map[string]any{"take_id": takes[0]}), "conflict")
}

func TestRepairTake_JobSucceedsWithTheNewTakeAndRefusesTakesThatNeedNoRepair(t *testing.T) {
	f := newFixture(t, shottest.Options{Verdict: shottest.FailVerdict})
	takes := f.Generate(t)
	assert.Contains(t, f.CallError("repair_take", map[string]any{"take_id": "missing"}), "not_found")

	var judged shotgen.TakeJobOutput
	f.CallOK("judge_take", map[string]any{"take_id": takes[0]}, &judged)
	waitJob(t, f, judged.JobID)
	f.WaitIdle(t)

	var out shotgen.TakeJobOutput
	f.CallOK("repair_take", map[string]any{"take_id": takes[0]}, &out)
	require.NotEmpty(t, out.JobID)
	st := waitJob(t, f, out.JobID)
	assert.Equal(t, mcpkit.JobSucceeded, st.State, st.Error)
	assert.NotEmpty(t, st.Result["new_take_id"])
	assert.NotContains(t, takes, st.Result["new_take_id"], "a new Take row")
	f.WaitIdle(t)
}

func TestRepairTake_NotJudgedAndAcceptedTakesAreConflicts(t *testing.T) {
	f := newFixture(t, shottest.Options{Verdict: shottest.PassVerdict})
	takes := f.Generate(t)
	assert.Contains(t, f.CallError("repair_take", map[string]any{"take_id": takes[0]}), "conflict", "never judged")

	var judged shotgen.TakeJobOutput
	f.CallOK("judge_take", map[string]any{"take_id": takes[0]}, &judged)
	waitJob(t, f, judged.JobID)
	f.WaitIdle(t)
	assert.Contains(t, f.CallError("repair_take", map[string]any{"take_id": takes[0]}), "conflict", "a Take that passed")
}

func TestGetJevShadowReport_ReturnsTheReport(t *testing.T) {
	f := newFixture(t, shottest.Options{})
	var out shotgen.ShadowReportOutput
	f.CallOK("get_jev_shadow_report", map[string]any{}, &out)
	assert.Zero(t, out.Report.Runs)
}
