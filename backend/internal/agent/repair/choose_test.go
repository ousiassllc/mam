package repair_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/repair"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// answerLLM returns a fixed answer for the Repair Agent.
type answerLLM struct{ text string }

func (a answerLLM) Complete(_ context.Context, agent llm.Agent, _ llm.Request) (llm.Response, error) {
	if agent != llm.AgentRepair {
		return llm.Response{}, assert.AnError
	}
	return llm.Response{Text: a.text}, nil
}

func TestChoose_TheAnswerMustBeACandidate(t *testing.T) {
	s := repair.Situation{
		Result:        domain.JudgeResult{Result: domain.JudgeFail, Issues: []domain.JudgeIssue{{Type: "camera_speed", Severity: domain.SeverityMedium}}},
		HasReferences: true, OtherModel: true,
	}
	cands := []domain.RepairAction{domain.RepairPrompt, domain.RepairFullRetake}

	c, err := repair.Choose(context.Background(), answerLLM{`{"action":"Prompt Repair","reason":"r","instructions":["Reduce Camera Speed","reduce_camera_speed"]}`}, s, cands)
	require.NoError(t, err)
	assert.Equal(t, domain.RepairPrompt, c.Action)
	assert.Equal(t, []string{"reduce_camera_speed"}, c.Instructions, "identifiers, without repeats")

	_, err = repair.Choose(context.Background(), answerLLM{`{"action":"different_model"}`}, s, cands)
	assert.Error(t, err, "a strategy outside the candidates is never used")
}
