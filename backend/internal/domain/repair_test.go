package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

func TestJudgeResult_AcceptedAppliesTheMinimumScoresOfTheRetryPolicy(t *testing.T) {
	tpl := domain.TemplateDrama
	policy := domain.ProjectPolicy{Quality: domain.QualityBalanced}.Effective() // overall 75, axis 55
	scores := func(v float64) map[domain.EvaluationAxis]float64 {
		m := map[domain.EvaluationAxis]float64{}
		for _, a := range tpl.Axes() {
			m[a] = v
		}
		return m
	}
	lowAxis := scores(90)
	lowAxis[domain.AxisEditingRhythm] = 40 // the overall score stays high, but one axis is under the minimum

	cases := []struct {
		name string
		res  domain.JudgeResult
		want bool
	}{
		{"pass without scores", domain.JudgeResult{Result: domain.JudgePass}, true},
		{"pass above both minimums", domain.JudgeResult{Result: domain.JudgePass, Scores: scores(80)}, true},
		{"pass under the overall minimum", domain.JudgeResult{Result: domain.JudgePass, Scores: scores(70)}, false},
		{"pass with one axis under the axis minimum", domain.JudgeResult{Result: domain.JudgePass, Scores: lowAxis}, false},
		{"fail is never accepted", domain.JudgeResult{Result: domain.JudgeFail, Scores: scores(99)}, false},
		{"a LOCK violation fails whatever the score is", domain.JudgeResult{
			Result: domain.JudgePass, Scores: scores(99),
			Issues: []domain.JudgeIssue{{Type: "wardrobe_change", Severity: domain.SeverityMedium, LockViolation: true}},
		}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { assert.Equal(t, c.want, c.res.Accepted(policy, tpl)) })
	}
}

func TestRepairHistory_CountsOnlyRepairsThatMadeATake(t *testing.T) {
	history := []domain.RepairRecord{
		{Action: domain.RepairPrompt, ResultingTakeID: "t2"},
		{Action: domain.RepairDifferentModel}, // failed before it made a Take: costs nothing
		{Action: domain.RepairEditLevel},      // regenerates nothing
		{Action: domain.RepairFullRetake, ResultingTakeID: "t3"},
	}
	assert.Equal(t, 2, domain.RetriesDone(history))
	assert.ElementsMatch(t,
		[]domain.RepairAction{domain.RepairPrompt, domain.RepairEditLevel, domain.RepairFullRetake},
		domain.TriedActions(history), "a repair that failed before making a Take is not tried")
}

func TestRepairAction_EditLevelIsTheOnlyOneThatDoesNotRegenerate(t *testing.T) {
	for _, a := range domain.RepairActions() {
		assert.Equal(t, a != domain.RepairEditLevel, a.Regenerates(), a)
	}
	assert.False(t, domain.RepairAction("unknown").Regenerates())
}
