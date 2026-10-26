package generation

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"bytes"
	"errors"
	"log/slog"
	"sync/atomic"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

func verdict(result domain.JudgeVerdict, confidence float64, issues ...domain.JudgeIssue) *domain.JudgeResult {
	return &domain.JudgeResult{Result: result, OverallConfidence: confidence, Issues: issues}
}

func issue(severity domain.IssueSeverity) domain.JudgeIssue {
	return domain.JudgeIssue{Severity: severity, Dimension: domain.DimProductAccuracy}
}

func TestBestTake_APassingTakeBeatsAFailingOneWhateverTheConfidence(t *testing.T) {
	all := []judged{{number: 1, result: verdict(domain.JudgeFail, 0.99)}, {number: 2, result: verdict(domain.JudgePass, 0.6)}}
	best, ok := bestTake(all, nil)
	require.True(t, ok)
	assert.Equal(t, 2, best.number)
}

func TestBestTake_AmongEqualVerdictsTheHigherTakeScoreThenTheEarlierTakeWins(t *testing.T) {
	cases := map[string]struct {
		all  []judged
		want int
	}{
		"fewer severe issues": {[]judged{
			{number: 1, result: verdict(domain.JudgeFail, 0.9, issue(domain.SeverityHigh))},
			{number: 2, result: verdict(domain.JudgeFail, 0.5, issue(domain.SeverityLow))},
		}, 2},
		"the confidence does not rank Takes": {[]judged{
			{number: 1, result: verdict(domain.JudgePass, 0.7)},
			{number: 2, result: verdict(domain.JudgePass, 0.9)},
		}, 1},
		"a full tie keeps the earlier take": {[]judged{
			{number: 3, result: verdict(domain.JudgePass, 0.8)},
			{number: 2, result: verdict(domain.JudgePass, 0.8)},
		}, 2},
	}
	for name, tc := range cases {
		best, ok := bestTake(tc.all, nil)
		require.True(t, ok, name)
		assert.Equal(t, tc.want, best.number, name)
	}
}

// The Judge picks the AI Pick (and in AUTO MODE the Selected Take) with domain.PickTake: the Best
// Take of a Candidate run must be the same Take, or the Shot's Take and its adopted Spec part ways.
func TestBestTake_ChoosesTheTakeDomainPickTakeChooses(t *testing.T) {
	cases := map[string][]judged{
		"two passing Takes, the later more confident": {
			{number: 1, result: verdict(domain.JudgePass, 0.6)},
			{number: 2, result: verdict(domain.JudgePass, 0.9)},
		},
		"two passing Takes, the later one with the lighter issue": {
			{number: 1, result: verdict(domain.JudgePass, 0.9, issue(domain.SeverityMedium))},
			{number: 2, result: verdict(domain.JudgePass, 0.5, issue(domain.SeverityLow))},
		},
		"a pass beats a more confident fail": {
			{number: 1, result: verdict(domain.JudgeFail, 0.99)},
			{number: 2, result: verdict(domain.JudgePass, 0.1, issue(domain.SeverityMedium))},
		},
	}
	for name, all := range cases {
		var takes []domain.TakeInfo
		for _, j := range all {
			scores := domain.ScoreTake(j.result.Issues)
			takes = append(takes, domain.TakeInfo{Take: domain.Take{ID: fmt.Sprint(j.number)}, Scores: &scores, Verdict: j.result.Result})
		}
		want, _, ok := domain.PickTake(takes)
		require.True(t, ok, name)
		best, ok := bestTake(all, nil)
		require.True(t, ok, name)
		assert.Equal(t, want, fmt.Sprint(best.number), name)
	}
}

// A pass whose scores are under the Minimum acceptance score is not accepted by the Retry Policy, so
// PickBest never selects it while some other Take is accepted: the Best Take must not be it either.
func TestBestTake_PrefersTheTakesTheRetryPolicyAcceptsLikePickBest(t *testing.T) {
	template := domain.TemplateProductCM
	eff := domain.ProjectPolicy{Quality: domain.QualityBalanced, Template: template}.Effective()
	weak := verdict(domain.JudgePass, 0.9) // no Issue: the best Take score, but the Judge scored it low
	weak.Scores = map[domain.EvaluationAxis]float64{}
	strong := verdict(domain.JudgePass, 0.9, issue(domain.SeverityLow)) // a minor Issue: a lower Take score
	strong.Scores = map[domain.EvaluationAxis]float64{}
	for _, a := range template.Axes() {
		weak.Scores[a], strong.Scores[a] = 1, 100
	}
	require.False(t, weak.Accepted(eff, template))
	require.True(t, strong.Accepted(eff, template))
	accepted := func(res domain.JudgeResult) bool { return res.Accepted(eff, template) }

	best, ok := bestTake([]judged{{number: 1, result: weak}, {number: 2, result: strong}}, accepted)
	require.True(t, ok)
	assert.Equal(t, 2, best.number, "the accepted Take wins although the other scores higher by its Issues")

	best, ok = bestTake([]judged{{number: 1, result: weak}, {number: 2, result: verdict(domain.JudgeFail, 0.9)}}, accepted)
	require.True(t, ok)
	assert.Equal(t, 1, best.number, "when none is accepted the best of all stands")
}

func TestBestTake_ATakeThatCouldNotBeJudgedIsNeverPreferredAndNoneJudgedMeansNoPick(t *testing.T) {
	best, ok := bestTake([]judged{{number: 1}, {number: 2, result: verdict(domain.JudgeFail, 0.1, issue(domain.SeverityHigh))}}, nil)
	require.True(t, ok)
	assert.Equal(t, 2, best.number, "even a bad verdict beats no verdict")

	_, ok = bestTake([]judged{{number: 1}, {number: 2}}, nil)
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

func TestAspectRatio_TakesEveryBriefAspectRatio(t *testing.T) {
	for _, r := range domain.BriefAspectRatios() {
		assert.Equal(t, r, aspectRatio(domain.Brief{AspectRatio: "縦 " + r}))
	}
}

func TestParallel_APanickingTaskBecomesItsErrorAndTheOthersFinish(t *testing.T) {
	var logs bytes.Buffer
	var ran atomic.Int32

	errs := parallel(slog.New(slog.NewTextHandler(&logs, nil)), 4, func(i int) error {
		ran.Add(1)
		switch i {
		case 1:
			panic("make take exploded")
		case 2:
			return errors.New("plain failure")
		}
		return nil
	})

	require.Len(t, errs, 4)
	assert.EqualValues(t, 4, ran.Load(), "every task ran")
	assert.NoError(t, errs[0])
	require.Error(t, errs[1])
	assert.Contains(t, errs[1].Error(), "make take exploded")
	assert.EqualError(t, errs[2], "plain failure")
	assert.NoError(t, errs[3])
	assert.Contains(t, logs.String(), "parallel task panicked")
	assert.Contains(t, logs.String(), "stack=")
}
