package judge_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/judge"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

func brandRig(t *testing.T, answer string) (*rig, judge.Input) {
	t.Helper()
	in := faceInput(t)
	in.FaceSubjects = nil
	in.Spec.Product = &domain.SpecProduct{ID: "Serum", Label: "BEST PRICE"}
	in.Brand = domain.BrandConstraints{
		{ID: "lock-1", Type: domain.BrandLogoRule, Level: domain.EnforceLock, Text: "Never distort the logo"},
		{ID: "adv-1", Type: domain.BrandPlatformRule, Level: domain.EnforceAdvisory, Text: "Keep the hook in the first 2 seconds"},
		{ID: "lock-2", Type: domain.BrandForbidden, Level: domain.EnforceLock, Text: "cheapest"},
	}
	return newRig(t, answer, false, steady()...), in
}

func TestEvaluate_TheBrandConstraintsAreGivenToTheModel(t *testing.T) {
	r, in := brandRig(t, passAnswer)
	r.evaluate(t, in)

	prompt := r.llm.requests[0].User
	for _, want := range []string{"BRAND / SAFETY CONSTRAINTS", "id=lock-1", "Never distort the logo", "id=adv-1", "ADVISORY", "brand_constraint_violation"} {
		assert.Contains(t, prompt, want)
	}
}

func TestEvaluate_ABrokenLockConstraintFailsTheTakeWhateverTheModelAndScoreSay(t *testing.T) {
	// The model says pass with a low severity note, and gives top scores.
	r, in := brandRig(t, `{"result":"pass","overall_confidence":0.99,"issues":[{"type":"logo","severity":"low","brand_constraint_id":"lock-1","time_range":"1.0-2.0s","description":"logo is warped"}]}`)
	res := r.evaluate(t, in)

	assert.Equal(t, domain.JudgeFail, res.Result)
	require.Len(t, res.Issues, 1)
	is := res.Issues[0]
	assert.Equal(t, domain.BrandIssueType, is.Type)
	assert.Equal(t, "lock-1", is.BrandConstraintID)
	assert.True(t, is.LockViolation)
	assert.Equal(t, domain.SeverityHigh, is.Severity)
	assert.Equal(t, "1.0-2.0s", is.TimeRange)
	assert.True(t, res.HasLockViolation())
}

func TestEvaluate_ABrokenAdvisoryConstraintIsOnlyAWarning(t *testing.T) {
	// Even a "high" severity from the model cannot fail the Take through an Advisory constraint.
	r, in := brandRig(t, `{"result":"pass","overall_confidence":0.9,"issues":[{"type":"hook","severity":"high","brand_constraint_id":"adv-1","description":"no hook"}]}`)
	res := r.evaluate(t, in)

	assert.Equal(t, domain.JudgePass, res.Result)
	require.Len(t, res.Issues, 1)
	assert.Equal(t, domain.BrandIssueType, res.Issues[0].Type)
	assert.Equal(t, domain.SeverityLow, res.Issues[0].Severity)
	assert.False(t, res.Issues[0].LockViolation)
}

func TestEvaluate_AnUnknownBrandConstraintIDIsAnOrdinaryIssue(t *testing.T) {
	r, in := brandRig(t, `{"result":"pass","overall_confidence":0.9,"issues":[{"type":"camera_speed","severity":"low","brand_constraint_id":"made-up"}]}`)
	res := r.evaluate(t, in)

	require.Len(t, res.Issues, 1)
	assert.Equal(t, "camera_speed", res.Issues[0].Type)
	assert.Empty(t, res.Issues[0].BrandConstraintID)
}

func TestEvaluate_AForbiddenExpressionInTheShotSpecFailsTheTakeWithoutTheModel(t *testing.T) {
	r, in := brandRig(t, passAnswer)
	in.Spec.ShotObjective = "The Cheapest serum on the market"
	res := r.evaluate(t, in)

	assert.Equal(t, domain.JudgeFail, res.Result)
	require.Len(t, res.Issues, 1)
	assert.Equal(t, "lock-2", res.Issues[0].BrandConstraintID)
	assert.True(t, res.Issues[0].LockViolation)
	assert.Contains(t, res.Issues[0].Description, "cheapest")
}
