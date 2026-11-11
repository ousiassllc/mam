package commandinterpreter_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

func usd(v float64) *float64 { return &v }

func TestInterpret_SetBudgetKeepsTheCeilingsTheDirectorNamed(t *testing.T) {
	cmd, f := interpret(t, `{"operations":[{"type":"set_budget","project_budget_usd":30}]}`,
		commandinterpreter.Input{Message: "予算を $30 に変えて", Budget: commandinterpreter.BudgetCeilings{Project: usd(20), Shot: usd(0.125)}})

	require.Len(t, cmd.Operations, 1)
	assert.Equal(t, commandinterpreter.Operation{Type: commandinterpreter.OpSetBudget, ProjectBudgetUSD: usd(30)}, cmd.Operations[0])
	assert.True(t, cmd.Operations[0].Confirms(), "a ceiling is money: the chat asks first")
	assert.Contains(t, f.system, `"set_budget"`, "the registry documents the operation in the prompt")
	assert.Contains(t, f.user, "project: $20 / shot: $0.125", "the ceilings now set are given to the model the way the announcement says them, not rounded to cents")
}

func TestInterpret_SetBudgetDropsWhatIsNoAmount(t *testing.T) {
	for name, answer := range map[string]string{
		"negative":   `{"operations":[{"type":"set_budget","project_budget_usd":-5}]}`,
		"no ceiling": `{"operations":[{"type":"set_budget"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			cmd, _ := interpret(t, answer, commandinterpreter.Input{Message: "予算を変えて"})
			assert.Empty(t, cmd.Operations)
		})
	}
	cmd, _ := interpret(t, `{"operations":[{"type":"set_budget","project_budget_usd":0,"shot_budget_usd":2.5}]}`, commandinterpreter.Input{Message: "上限を外して、1 ショット $2.5 まで"})
	require.Len(t, cmd.Operations, 1)
	assert.Equal(t, usd(0), cmd.Operations[0].ProjectBudgetUSD, "0 removes the ceiling")
	assert.Equal(t, usd(2.5), cmd.Operations[0].ShotBudgetUSD)
}

func TestInterpret_OperationsThatAreNotInThePromptAreNeverInvented(t *testing.T) {
	cmd, _ := interpret(t, `{"operations":[{"type":"set_quality_policy","quality_policy":"fast"}]}`, commandinterpreter.Input{Message: "安くして"})
	assert.Empty(t, cmd.Operations, "set_quality_policy is only the action of a card button")

	op, ok := commandinterpreter.Normalize(commandinterpreter.Operation{Type: commandinterpreter.OpSetQualityPolicy, Quality: domain.QualityFast})
	assert.True(t, ok)
	assert.Equal(t, domain.QualityFast, op.Quality)
	_, ok = commandinterpreter.Normalize(commandinterpreter.Operation{Type: commandinterpreter.OpSetQualityPolicy, Quality: "cheapest"})
	assert.False(t, ok)

	mode, ok := commandinterpreter.Normalize(commandinterpreter.Operation{Type: commandinterpreter.OpSetMode, ProjectMode: domain.ModeDirector})
	assert.True(t, ok, "the mode switch is the action of a button of the budget Escalation")
	assert.Equal(t, domain.ModeDirector, mode.ProjectMode)
	_, ok = commandinterpreter.Normalize(commandinterpreter.Operation{Type: commandinterpreter.OpSetMode, ProjectMode: "manual"})
	assert.False(t, ok)
	_, ok = commandinterpreter.Normalize(commandinterpreter.Operation{Type: commandinterpreter.OpSetLock, Parameter: domain.ParamCamera, Mode: domain.LockLocked})
	assert.False(t, ok, "an operation outside the registry is not one a card carries out")
}

func TestUnaskedDropsASettingTheMessageDoesNotMention(t *testing.T) {
	ops := []commandinterpreter.Operation{
		{Type: commandinterpreter.OpSetBudget, ProjectBudgetUSD: usd(30)},
		{Type: commandinterpreter.OpSetLock, Parameter: domain.ParamCamera, Mode: domain.LockLocked},
	}
	assert.Len(t, commandinterpreter.Unasked(ops, "カメラを固定して"), 1, "the model's word alone does not change the budget")
	assert.Len(t, commandinterpreter.Unasked(ops, "予算を $30 に変えて、カメラは固定"), 2)
}

func TestAsksAnyModeIsTheBudgetNotTheNote(t *testing.T) {
	assert.True(t, commandinterpreter.AsksAnyMode("予算を $30 に変えて"))
	assert.False(t, commandinterpreter.AsksAnyMode("これ覚えておいて"), "the note has its own check")
	assert.False(t, commandinterpreter.AsksAnyMode("カメラを固定して"))
}
