package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestScoreTake_SeverityTakesPenaltyOffTheAxisOfTheDimension(t *testing.T) {
	scores := ScoreTake([]JudgeIssue{
		{Type: "logo", Severity: SeverityHigh, Dimension: DimProductAccuracy},
		{Type: "speed", Severity: SeverityMedium, Dimension: DimCameraMotion},
		{Type: "jump", Severity: SeverityLow, Dimension: DimTemporal},
		{Type: "jump2", Severity: SeverityLow, Dimension: DimFlicker},
		{Type: "prompt", Severity: SeverityHigh, Dimension: DimPromptAdherence},
	})

	assert.Equal(t, TakeScores{Product: 65, Acting: 100, Camera: 85, Continuity: 90}, scores)
}

func TestScoreTake_FaceIdentityDriftCountsForActingAndScoresNeverGoBelowZero(t *testing.T) {
	issues := []JudgeIssue{{Type: IssueTypeFaceIdentityDrift, Severity: SeverityHigh}}
	for range 4 {
		issues = append(issues, JudgeIssue{Type: "x", Severity: SeverityHigh, Dimension: DimActing})
	}

	scores := ScoreTake(issues)

	assert.Zero(t, scores.Acting)
	assert.Equal(t, 100.0, scores.Product)
}

func TestScoreTake_NoIssuesIsPerfect(t *testing.T) {
	assert.Equal(t, TakeScores{100, 100, 100, 100}, ScoreTake(nil))
	assert.Equal(t, 100.0, ScoreTake(nil).Overall())
}

func TestPickTake_PassedTakeBeatsAHigherScoringFailedTake(t *testing.T) {
	takes := []TakeInfo{
		{Take: Take{ID: "a", Status: TakeCandidate}, Scores: &TakeScores{100, 100, 100, 100}, Verdict: JudgeFail},
		{Take: Take{ID: "b", Status: TakeCandidate}, Scores: &TakeScores{80, 80, 80, 80}, Verdict: JudgePass},
		{Take: Take{ID: "c", Status: TakeCandidate}, Scores: &TakeScores{90, 90, 90, 90}, Verdict: JudgePass},
	}

	id, passed, ok := PickTake(takes)

	assert.True(t, ok)
	assert.True(t, passed)
	assert.Equal(t, "c", id)
}

func TestPickTake_TieGoesToTheOlderTakeAndRejectedOrUnjudgedTakesNeverWin(t *testing.T) {
	takes := []TakeInfo{
		{Take: Take{ID: "rejected", Status: TakeRejected}, Scores: &TakeScores{100, 100, 100, 100}, Verdict: JudgePass},
		{Take: Take{ID: "unjudged", Status: TakeCandidate}},
		{Take: Take{ID: "old", Status: TakeCandidate}, Scores: &TakeScores{90, 90, 90, 90}, Verdict: JudgePass},
		{Take: Take{ID: "new", Status: TakeCandidate}, Scores: &TakeScores{90, 90, 90, 90}, Verdict: JudgePass},
	}

	id, _, ok := PickTake(takes)

	assert.True(t, ok)
	assert.Equal(t, "old", id)
}

func TestPickTake_NothingJudgedPicksNothingAndAllFailedIsReportedAsNotPassed(t *testing.T) {
	_, _, ok := PickTake([]TakeInfo{{Take: Take{ID: "a"}}})
	assert.False(t, ok)

	id, passed, ok := PickTake([]TakeInfo{{Take: Take{ID: "a"}, Scores: &TakeScores{50, 50, 50, 50}, Verdict: JudgeFail}})
	assert.True(t, ok)
	assert.False(t, passed)
	assert.Equal(t, "a", id)
}

func TestResolutionHeight_And_Proxied(t *testing.T) {
	for res, want := range map[string]int{"1080p": 1080, "720p": 720, "2160p": 2160, "4K": 2160, "4k": 2160, "": 0, "wide": 0} {
		assert.Equal(t, want, ResolutionHeight(res), res)
	}
	assert.False(t, TakeInfo{Resolution: "1080p"}.Proxied())
	assert.True(t, TakeInfo{Resolution: "4K"}.Proxied())
	assert.False(t, TakeInfo{}.Proxied())
}
