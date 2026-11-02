package gentest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	mcpgentest "github.com/ousiassllc/mam/backend/internal/mcp/tools/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/match"
)

// matchRig is an MCP rig whose generation test has the prompt match over a fake LLM that scores the rounds 40, 60 and 90.
func matchRig(t *testing.T, opts ...gentesttest.Option) (*rig, *gentesttest.FakeMatchLLM) {
	t.Helper()
	r := newRig(t, opts...)
	f := &gentesttest.FakeMatchLLM{Scores: []int{40, 60, 90}, Err: map[string]error{}}
	r.GT.UseMatch(f)
	return r, f
}

func (r *rig) matchArgs(extra map[string]any) map[string]any {
	id, _, err := r.GT.Svc.StageReference(gentesttest.PNG)
	require.NoError(r.T, err)
	args := map[string]any{"reference": map[string]any{"source": "upload", "id": id}, "model": "flux-2", "target_score": 85, "max_rounds": 4}
	for k, v := range extra {
		args[k] = v
	}
	return args
}

func TestPromptMatchToolsFollowTheEstimateConfirmRunFlow(t *testing.T) {
	r, f := matchRig(t)
	args := r.matchArgs(nil)

	var est match.Estimate
	r.CallOK("estimate_prompt_match", args, &est)
	assert.Equal(t, 4, est.Rounds)
	assert.InDelta(t, 4*0.03, est.TotalUSD, 1e-9, "the most rounds times the price of one picture")
	assert.Equal(t, match.LLMLine, est.Breakdown[1].Model)
	assert.Zero(t, f.Count(gentesttest.CallDescribe), "an estimate calls no LLM")
	assert.Empty(t, r.GT.Fake.Calls(), "and generates nothing")

	run := map[string]any{"estimate_id": est.ID, "confirmed_total_usd": est.TotalUSD}
	for k, v := range args {
		run[k] = v
	}
	var out mcpgentest.RunOutput
	r.CallOK("run_prompt_match", run, &out)
	assert.Equal(t, 4, out.TotalItems)
	st := r.job(out.JobID)
	require.Equal(t, mcpkit.JobSucceeded, st.State)

	var v match.View
	r.CallOK("get_prompt_match", map[string]any{"batch_id": out.BatchID}, &v)
	assert.Equal(t, match.StopReached, v.Stop)
	require.Len(t, v.Rounds, 3)
	assert.Equal(t, 3, v.Best)
	assert.Equal(t, "prompt 1, fix 1, fix 2", v.BestPrompt)
	assert.Equal(t, []string{"差 2"}, v.Rounds[1].Differences)
	assert.Equal(t, "直した 1", v.Rounds[1].Change)
	assert.Equal(t, "prompt 1", v.Rounds[1].BasePrompt)

	// the pictures are results of the history, and the batch of the generation test says the same
	var b struct {
		State    string `json:"state"`
		Finished bool   `json:"finished"`
		Total    int    `json:"total"`
	}
	r.CallOK("get_generation_test", map[string]any{"batch_id": out.BatchID}, &b)
	assert.Equal(t, "succeeded", b.State)
	assert.True(t, b.Finished)
	assert.Equal(t, 3, b.Total)
}

func TestPromptMatchRunRefusesAnEstimateOverTheLimitAndAChangedOne(t *testing.T) {
	r, f := matchRig(t, gentesttest.WithConfig(func(c *modeladapter.Config) { cost := 0.05; c.Gentest.MaxCostUSD = &cost }))
	args := r.matchArgs(nil) // 4 x 0.03 > 0.05
	var est match.Estimate
	r.CallOK("estimate_prompt_match", args, &est)
	assert.NotEmpty(t, est.OverLimit)

	run := map[string]any{"estimate_id": est.ID, "confirmed_total_usd": est.TotalUSD}
	for k, v := range args {
		run[k] = v
	}
	assert.Contains(t, r.CallError("run_prompt_match", run), "conflict")
	run["confirmed_total_usd"] = 0.0
	assert.Contains(t, r.CallError("run_prompt_match", run), "conflict")
	assert.Zero(t, f.Count(gentesttest.CallDescribe))
	assert.Empty(t, r.GT.Fake.Calls(), "a match over the limit does not start")
}

func TestPromptMatchRunRefusesAnEstimateOverTheCostTheCallerAllowed(t *testing.T) {
	r, f := matchRig(t)
	args := r.matchArgs(map[string]any{"max_cost_usd": 0.05}) // 4 x 0.03
	var est match.Estimate
	r.CallOK("estimate_prompt_match", args, &est)
	assert.Contains(t, est.OverLimit, "指定した上限の費用")
	run := map[string]any{"estimate_id": est.ID, "confirmed_total_usd": est.TotalUSD}
	for k, v := range args {
		run[k] = v
	}
	assert.Contains(t, r.CallError("run_prompt_match", run), "conflict")
	assert.Zero(t, f.Count(gentesttest.CallDescribe))
	assert.Empty(t, r.GT.Fake.Calls())
}

func TestPromptMatchRefusesWhatCannotRun(t *testing.T) {
	r, _ := matchRig(t)
	assert.Contains(t, r.CallError("estimate_prompt_match", r.matchArgs(map[string]any{"model": "kling-3.0"})), "validation")
	assert.Contains(t, r.CallError("estimate_prompt_match", r.matchArgs(map[string]any{"mode": "sometimes"})), "validation")
	assert.Contains(t, r.CallError("estimate_prompt_match", map[string]any{"model": "flux-2"}), "validation", "a reference image is required")
}

func TestPromptMatchGoesOnWithWordsAndInstructRefusesAFinishedOne(t *testing.T) {
	r, _ := matchRig(t)
	first := r.matchArgs(map[string]any{"mode": "adjust"})
	var est match.Estimate
	r.CallOK("estimate_prompt_match", first, &est)
	run := map[string]any{"estimate_id": est.ID, "confirmed_total_usd": est.TotalUSD}
	for k, v := range first {
		run[k] = v
	}
	var out mcpgentest.RunOutput
	r.CallOK("run_prompt_match", run, &out)
	require.Equal(t, mcpkit.JobSucceeded, r.job(out.JobID).State)

	more := map[string]any{"continue_batch_id": out.BatchID, "mode": "adjust", "instruction": "背景は海に"}
	r.CallOK("estimate_prompt_match", more, &est)
	run = map[string]any{"estimate_id": est.ID, "confirmed_total_usd": est.TotalUSD}
	for k, v := range more {
		run[k] = v
	}
	var again mcpgentest.RunOutput
	r.CallOK("run_prompt_match", run, &again)
	assert.Equal(t, out.BatchID, again.BatchID)
	require.Equal(t, mcpkit.JobSucceeded, r.job(again.JobID).State)

	var v match.View
	r.CallOK("get_prompt_match", map[string]any{"batch_id": out.BatchID}, &v)
	require.Len(t, v.Rounds, 2)
	assert.Equal(t, "背景は海に", v.Rounds[1].Instruction)
	assert.Equal(t, 2, v.Rounds[1].Round)

	assert.Contains(t, r.CallError("instruct_prompt_match", map[string]any{"batch_id": out.BatchID, "instruction": "あとから"}), "conflict")
	assert.Contains(t, r.CallError("instruct_prompt_match", map[string]any{"batch_id": "nope", "instruction": "x"}), "not_found")
}
