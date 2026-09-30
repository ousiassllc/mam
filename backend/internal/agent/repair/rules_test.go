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

func issue(typ string, sev domain.IssueSeverity, timeRange string) domain.JudgeIssue {
	return domain.JudgeIssue{Type: typ, Severity: sev, TimeRange: timeRange}
}

func open() repair.Situation {
	return repair.Situation{
		Result:        domain.JudgeResult{Result: domain.JudgeFail, Issues: []domain.JudgeIssue{issue("camera_speed", domain.SeverityMedium, "")}},
		HasReferences: true, OtherModel: true,
	}
}

func TestCandidates_OnlyStrategiesThatCanBeCarriedOutRemain(t *testing.T) {
	all := domain.RepairActions()
	minusEdit := func(skip ...domain.RepairAction) []domain.RepairAction {
		var out []domain.RepairAction
		for _, a := range all {
			if a != domain.RepairEditLevel && !contains(skip, a) {
				out = append(out, a)
			}
		}
		return out
	}

	s := open()
	assert.Equal(t, minusEdit(), repair.Candidates(s), "Edit-Level Repair needs a time range")

	s.PromptLocked = true
	assert.Equal(t, minusEdit(domain.RepairPrompt, domain.RepairPartialRegeneration), repair.Candidates(s), "LOCKed Prompt cannot be rewritten")

	s = open()
	s.HasReferences, s.OtherModel = false, false
	assert.Equal(t, minusEdit(domain.RepairReferenceReinforce, domain.RepairDifferentModel), repair.Candidates(s))

	s = open()
	s.Result.Issues = []domain.JudgeIssue{issue("flicker", domain.SeverityLow, "1.0-1.5s")}
	assert.Contains(t, repair.Candidates(s), domain.RepairEditLevel, "a short minor defect can be fixed in the edit")

	s.Result.Issues[0].Severity = domain.SeverityHigh
	assert.NotContains(t, repair.Candidates(s), domain.RepairEditLevel)

	s.Result.Issues[0].Severity, s.Result.Issues[0].LockViolation = domain.SeverityMedium, true
	assert.NotContains(t, repair.Candidates(s), domain.RepairEditLevel, "a LOCK violation is never left to the edit")
}

func TestCandidates_ATriedStrategyIsNotRepeatedExceptFullRetake(t *testing.T) {
	s := open()
	s.Tried = []domain.RepairAction{domain.RepairPrompt, domain.RepairFullRetake}
	got := repair.Candidates(s)
	assert.NotContains(t, got, domain.RepairPrompt)
	assert.Contains(t, got, domain.RepairFullRetake, "a new generation of the same spec is a new draw")
}

func TestCandidates_FullRetakeIsAlwaysLeft(t *testing.T) {
	s := repair.Situation{PromptLocked: true, Tried: domain.RepairActions()}
	assert.Equal(t, []domain.RepairAction{domain.RepairFullRetake}, repair.Candidates(s))
}

func TestRule_FaceIdentityDriftPrefersReferenceReinforcement(t *testing.T) {
	s := open()
	s.Result.Issues = []domain.JudgeIssue{issue(domain.IssueTypeFaceIdentityDrift, domain.SeverityHigh, "1.5-3.0s")}
	action, _, ok := repair.Rule(s, repair.Candidates(s))
	require.True(t, ok)
	assert.Equal(t, domain.RepairReferenceReinforce, action)

	// Without References there is nothing to reinforce: the Repair Agent chooses.
	s.HasReferences = false
	_, _, ok = repair.Rule(s, repair.Candidates(s))
	assert.False(t, ok)
}

func TestRule_ASingleCandidateIsTakenAndOtherwiseTheAgentChooses(t *testing.T) {
	action, _, ok := repair.Rule(open(), []domain.RepairAction{domain.RepairFullRetake})
	require.True(t, ok)
	assert.Equal(t, domain.RepairFullRetake, action)

	_, _, ok = repair.Rule(open(), repair.Candidates(open()))
	assert.False(t, ok, "camera_speed leaves a real choice")
}

func TestFallback_UsesTheJudgesRecommendationWhenItIsACandidate(t *testing.T) {
	s := open()
	s.Result.RepairStrategy = &domain.RepairStrategy{Action: domain.RepairPartialRegeneration}
	action, _ := repair.Fallback(s, repair.Candidates(s))
	assert.Equal(t, domain.RepairPartialRegeneration, action)

	s.PromptLocked = true // the recommendation is no candidate any more
	action, _ = repair.Fallback(s, repair.Candidates(s))
	assert.Equal(t, domain.RepairFullRetake, action)
}

func TestScored_BeatsPrefersNoLockViolationThenScoreThenFewerHighIssues(t *testing.T) {
	assert.True(t, repair.Scored{Score: 10, HasScore: true}.Beats(repair.Scored{Score: 90, HasScore: true, Lock: true}))
	assert.True(t, repair.Scored{Score: 80, HasScore: true}.Beats(repair.Scored{Score: 70, HasScore: true}))
	assert.True(t, repair.Scored{Score: 1, HasScore: true}.Beats(repair.Scored{}), "a scored Take beats an unscored one")
	assert.True(t, repair.Scored{High: 0}.Beats(repair.Scored{High: 2}))
	assert.False(t, repair.Scored{High: 1}.Beats(repair.Scored{High: 1}), "a tie is not a win")
}

// answerLLM returns a fixed answer for the Repair Agent.
type answerLLM struct{ text string }

func (a answerLLM) Complete(_ context.Context, agent llm.Agent, _ llm.Request) (llm.Response, error) {
	if agent != llm.AgentRepair {
		return llm.Response{}, assert.AnError
	}
	return llm.Response{Text: a.text}, nil
}

func TestChoose_TheAnswerMustBeACandidate(t *testing.T) {
	s := open()
	cands := []domain.RepairAction{domain.RepairPrompt, domain.RepairFullRetake}

	c, err := repair.Choose(context.Background(), answerLLM{`{"action":"Prompt Repair","reason":"r","instructions":["Reduce Camera Speed","reduce_camera_speed"]}`}, s, cands)
	require.NoError(t, err)
	assert.Equal(t, domain.RepairPrompt, c.Action)
	assert.Equal(t, []string{"reduce_camera_speed"}, c.Instructions, "identifiers, without repeats")

	_, err = repair.Choose(context.Background(), answerLLM{`{"action":"different_model"}`}, s, cands)
	assert.Error(t, err, "a strategy outside the candidates is never used")
}

func contains(list []domain.RepairAction, a domain.RepairAction) bool {
	for _, x := range list {
		if x == a {
			return true
		}
	}
	return false
}
