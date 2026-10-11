package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func shotTakes() []TakeInfo {
	return []TakeInfo{
		{Take: Take{ID: "t1", FilePath: "p/1.mp4"}, DurationSeconds: 4},
		{Take: Take{ID: "t2", FilePath: "p/2.mp4"}, DurationSeconds: 4},
		{Take: Take{ID: "t3", FilePath: "p/3.mp4", Status: TakeRejected}, DurationSeconds: 4},
	}
}

func TestParseTakeSegments_ReadsLettersNumbersAndSeparatorsInTheOrderOfTheText(t *testing.T) {
	segs, err := ParseTakeSegments("Take B の 2-4秒 + Take A の 0〜2秒", shotTakes())
	require.NoError(t, err)
	assert.Equal(t, []TakeSegment{{TakeID: "t2", Start: 2, End: 4}, {TakeID: "t1", Start: 0, End: 2}}, segs)

	segs, err = ParseTakeSegments("take 1: 0.5 - 1.5s, take 2: 1.5から3.25秒", shotTakes())
	require.NoError(t, err)
	assert.Equal(t, []TakeSegment{{TakeID: "t1", Start: 0.5, End: 1.5}, {TakeID: "t2", Start: 1.5, End: 3.25}}, segs)
}

func TestParseTakeSegments_UnreadableTextAndUnknownTakesAreErrors(t *testing.T) {
	_, err := ParseTakeSegments("いい感じに繋いで", shotTakes())
	assert.Error(t, err)

	_, err = ParseTakeSegments("Take A の 0-2秒 + Take D の 2-4秒", shotTakes())
	assert.ErrorContains(t, err, "Take D")
}

func TestValidateTakeSegments_RejectsWhatCannotBeComposed(t *testing.T) {
	ok := []TakeSegment{{TakeID: "t1", Start: 0, End: 2}, {TakeID: "t2", Start: 2, End: 4}}
	assert.NoError(t, ValidateTakeSegments(ok, shotTakes()))
	assert.NoError(t, ValidateTakeSegments([]TakeSegment{ok[0], {TakeID: "t2", Start: 0.4, End: 0.7}}, shotTakes())) // 0.29999999999999993 long

	for name, segs := range map[string][]TakeSegment{
		"one segment":     ok[:1],
		"unknown take":    {ok[0], {TakeID: "x", Start: 0, End: 2}},
		"rejected take":   {ok[0], {TakeID: "t3", Start: 0, End: 2}},
		"too short":       {ok[0], {TakeID: "t2", Start: 1, End: 1.1}},
		"negative start":  {ok[0], {TakeID: "t2", Start: -1, End: 2}},
		"beyond the take": {ok[0], {TakeID: "t2", Start: 2, End: 5}},
		"too many parts":  append(make([]TakeSegment, 0), ok[0], ok[1], ok[0], ok[1], ok[0], ok[1], ok[0], ok[1], ok[0]),
	} {
		assert.Error(t, ValidateTakeSegments(segs, shotTakes()), name)
	}
}

func TestTakeLabel(t *testing.T) {
	assert.Equal(t, []string{"A", "B", "Z", "27"}, []string{TakeLabel(0), TakeLabel(1), TakeLabel(25), TakeLabel(26)})
}

func TestPickAcceptedTake_ARejectedTakeDoesNotMakeTheOthersFallShortOfThePolicy(t *testing.T) {
	scored := &TakeScores{80, 80, 80, 80}
	takes := []TakeInfo{
		{Take: Take{ID: "a", Status: TakeRejected}, Scores: scored, Verdict: JudgePass},
		{Take: Take{ID: "b"}, Scores: scored, Verdict: JudgeFail},
		{Take: Take{ID: "c"}, Scores: &TakeScores{90, 90, 90, 90}, Verdict: JudgeFail},
	}
	onlyA := func(id string) bool { return id == "a" }

	id, passed, ok := PickAcceptedTake(takes, onlyA)
	assert.True(t, ok)
	assert.Equal(t, "c", id)
	assert.False(t, passed)

	id, _, _ = PickAcceptedTake(takes, func(id string) bool { return id == "b" })
	assert.Equal(t, "b", id, "an accepted Take that is not rejected still narrows the pool")
	id, _, _ = PickAcceptedTake(takes, nil)
	assert.Equal(t, "c", id)
	_, _, ok = PickAcceptedTake(takes[:1], nil)
	assert.False(t, ok, "only a rejected Take: nothing to pick")
}

func TestTakeInfo_UsableAndSelectableFollowStatusAndArchivedFile(t *testing.T) {
	take := func(status TakeStatus, file string) TakeInfo {
		return TakeInfo{Take: Take{Status: status, FilePath: file}}
	}
	cases := []struct {
		name               string
		take               TakeInfo
		usable, selectable bool
	}{
		{"candidate", take(TakeCandidate, "a.mp4"), true, true},
		{"ai pick", take(TakeAIPick, "a.mp4"), true, true},
		{"selected", take(TakeSelected, "a.mp4"), true, false},
		{"rejected", take(TakeRejected, "a.mp4"), false, false},
		{"archived", take(TakeCandidate, ""), false, false},
	}
	for _, c := range cases {
		assert.Equal(t, c.usable, c.take.Usable(), c.name)
		assert.Equal(t, c.selectable, c.take.Selectable(), c.name)
	}
}
