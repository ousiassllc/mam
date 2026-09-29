package generation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

func verdict(result domain.JudgeVerdict, confidence float64, issues ...domain.JudgeIssue) *domain.JudgeResult {
	return &domain.JudgeResult{Result: result, OverallConfidence: confidence, Issues: issues}
}

func issue(severity domain.IssueSeverity) domain.JudgeIssue {
	return domain.JudgeIssue{Severity: severity}
}

func TestBestTake_APassingTakeBeatsAFailingOneWhateverTheConfidence(t *testing.T) {
	all := []judged{
		{number: 1, result: verdict(domain.JudgeFail, 0.99)},
		{number: 2, result: verdict(domain.JudgePass, 0.6)},
	}
	best, ok := bestTake(all)
	require.True(t, ok)
	assert.Equal(t, 2, best.number)
}

func TestBestTake_AmongEqualVerdictsTheLighterIssuesThenTheHigherConfidenceThenTheEarlierTakeWin(t *testing.T) {
	lock := domain.JudgeIssue{Severity: domain.SeverityHigh, LockViolation: true}
	cases := map[string]struct {
		all  []judged
		want int
	}{
		"fewer severe issues": {[]judged{
			{number: 1, result: verdict(domain.JudgeFail, 0.9, issue(domain.SeverityHigh))},
			{number: 2, result: verdict(domain.JudgeFail, 0.5, issue(domain.SeverityLow))},
		}, 2},
		"a LOCK violation is worse than a plain high severity issue": {[]judged{
			{number: 1, result: verdict(domain.JudgeFail, 0.9, lock)},
			{number: 2, result: verdict(domain.JudgeFail, 0.5, issue(domain.SeverityHigh))},
		}, 2},
		"same issues, higher confidence": {[]judged{
			{number: 1, result: verdict(domain.JudgePass, 0.7)},
			{number: 2, result: verdict(domain.JudgePass, 0.9)},
		}, 2},
		"a full tie keeps the earlier take": {[]judged{
			{number: 3, result: verdict(domain.JudgePass, 0.8)},
			{number: 2, result: verdict(domain.JudgePass, 0.8)},
		}, 2},
	}
	for name, tc := range cases {
		best, ok := bestTake(tc.all)
		require.True(t, ok, name)
		assert.Equal(t, tc.want, best.number, name)
	}
}

func TestBestTake_ATakeThatCouldNotBeJudgedIsNeverPreferredAndNoneJudgedMeansNoPick(t *testing.T) {
	best, ok := bestTake([]judged{{number: 1}, {number: 2, result: verdict(domain.JudgeFail, 0.1, issue(domain.SeverityHigh))}})
	require.True(t, ok)
	assert.Equal(t, 2, best.number, "even a bad verdict beats no verdict")

	_, ok = bestTake([]judged{{number: 1}, {number: 2}})
	assert.False(t, ok)
}

func TestSevere_OnlyAFailWithAHighSeverityIssueOrALockViolationIsSevere(t *testing.T) {
	assert.True(t, severe(*verdict(domain.JudgeFail, 0.5, issue(domain.SeverityHigh))))
	assert.True(t, severe(*verdict(domain.JudgeFail, 0.5, domain.JudgeIssue{Severity: domain.SeverityMedium, LockViolation: true})))
	assert.False(t, severe(*verdict(domain.JudgeFail, 0.5, issue(domain.SeverityMedium), issue(domain.SeverityLow))))
	assert.False(t, severe(*verdict(domain.JudgeFail, 0.5)))
	assert.False(t, severe(*verdict(domain.JudgePass, 0.9, issue(domain.SeverityHigh))), "the Judge decides pass and fail; a pass is never retaken")
}

func TestPlannedTakes_FollowsTheModeAndTheRetryPolicyOfTheQualityPolicy(t *testing.T) {
	plan := func(q domain.QualityPolicy, takes int) int {
		eff := domain.ProjectPolicy{Quality: q}.Effective()
		return plannedTakes(domain.ModeOf(q), eff, Strategy{Takes: takes})
	}
	// MAX QUALITY (Quality Mode): every strategy at once, fewer when the budget allows fewer Takes.
	assert.Equal(t, 3, plan(domain.QualityMaxQuality, 4))
	assert.Equal(t, 2, plan(domain.QualityMaxQuality, 2))
	assert.Equal(t, 1, plan(domain.QualityMaxQuality, 0), "at least one")
	// BALANCED: the first Candidate plus the retries (2), up to one per strategy.
	assert.Equal(t, 3, plan(domain.QualityBalanced, 1))
	// FAST (Economy Mode): one Take plus the retries (1).
	assert.Equal(t, 2, plan(domain.QualityFast, 2))
}

func TestModeOf_TiesTheGenerationModeToTheQualityPolicy(t *testing.T) {
	assert.Equal(t, domain.ModeQuality, domain.ModeOf(domain.QualityMaxQuality))
	assert.Equal(t, domain.ModeBalanced, domain.ModeOf(domain.QualityBalanced))
	assert.Equal(t, domain.ModeEconomy, domain.ModeOf(domain.QualityFast))
}
