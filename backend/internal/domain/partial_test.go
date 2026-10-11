package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIssueRanges_AllIssuesNeedARange(t *testing.T) {
	ranges, ok := IssueRanges([]JudgeIssue{{TimeRange: "2.1-2.8s"}, {TimeRange: "0.5-1.0s"}})
	require.True(t, ok)
	assert.Equal(t, []TimeRange{{2.1, 2.8}, {0.5, 1.0}}, ranges)

	_, ok = IssueRanges([]JudgeIssue{{TimeRange: "2.1-2.8s"}, {Type: "prompt_adherence"}})
	assert.False(t, ok, "an Issue of the whole Shot cannot be fixed by regenerating a stretch")
	_, ok = IssueRanges([]JudgeIssue{{TimeRange: "3.0-2.0s"}})
	assert.False(t, ok)
	_, ok = IssueRanges(nil)
	assert.False(t, ok)
}

func TestParseTimeRanges_RejectsUnreadableAndOutOfShotRanges(t *testing.T) {
	ranges, err := ParseTimeRanges([]string{"1.0-2.5", " 3-4s ", "0.4-0.7"}, 5) // 0.7 - 0.4 is 0.29999999999999993
	require.NoError(t, err)
	assert.Equal(t, []TimeRange{{1, 2.5}, {3, 4}, {0.4, 0.7}}, ranges)

	for _, bad := range [][]string{nil, {"soon"}, {"2.0-2.1"}, {"6-7"}, {"-1-2"}} {
		_, err := ParseTimeRanges(bad, 5)
		assert.Error(t, err, "%v", bad)
	}
}

func TestSpliceSegments_ReplacesTheRangeAndKeepsTheRestOfTheBaseTake(t *testing.T) {
	segs := SpliceSegments("base", 5, "new", 5, []TimeRange{{2, 3}})

	assert.Equal(t, []TakeSegment{
		{TakeID: "base", Start: 0, End: 1.8},
		{TakeID: "new", Start: 1.8, End: 3.2},
		{TakeID: "base", Start: 3.2, End: 5},
	}, segs)
}

func TestSpliceSegments_MergesCloseRangesAndAbsorbsShortEnds(t *testing.T) {
	// 1.0-1.5 and 1.7-2.0 leave no room for the base Take between them; the head (0-0.1 after
	// padding) and the tail (4.9-5.0) are too short to show.
	segs := SpliceSegments("base", 5, "new", 5, []TimeRange{{1.7, 2.0}, {1.0, 1.5}, {4.8, 5.0}})

	assert.Equal(t, []TakeSegment{
		{TakeID: "base", Start: 0, End: 0.8},
		{TakeID: "new", Start: 0.8, End: 2.2},
		{TakeID: "base", Start: 2.2, End: 4.6},
		{TakeID: "new", Start: 4.6, End: 5},
	}, segs)

	head := SpliceSegments("base", 5, "new", 5, []TimeRange{{0, 0.1}})
	require.Len(t, head, 2)
	assert.Equal(t, "new", head[0].TakeID)
	assert.Zero(t, head[0].Start)
	assert.InDelta(t, 0.3, head[0].End, 1e-9)
}

func TestSpliceSegments_ShorterTakeBoundsTheResult(t *testing.T) {
	segs := SpliceSegments("base", 5, "new", 4, []TimeRange{{2, 3}})

	require.Len(t, segs, 3)
	assert.Equal(t, 4.0, segs[2].End)
}

func TestSpliceSegments_NoSpliceWhenNothingOfTheBaseTakeRemains(t *testing.T) {
	assert.Nil(t, SpliceSegments("base", 4, "new", 4, []TimeRange{{0.1, 3.9}}), "the whole Shot is regenerated")
	assert.Nil(t, SpliceSegments("base", 4, "new", 4, nil))
	assert.Nil(t, SpliceSegments("base", 0.5, "new", 0.5, []TimeRange{{0, 0.3}}), "too short to cut")
	assert.Nil(t, SpliceSegments("base", 0, "new", 0, []TimeRange{{1, 2}}), "unknown length")
}

func TestSpliceSegments_TooManyPartsIsNoSplice(t *testing.T) {
	var ranges []TimeRange
	for i := range 5 {
		from := 1 + float64(i)*2
		ranges = append(ranges, TimeRange{From: from, To: from + 0.5})
	}

	assert.Nil(t, SpliceSegments("base", 12, "new", 12, ranges), "5 ranges need 11 parts, the limit is 8")
	assert.Len(t, SpliceSegments("base", 12, "new", 12, ranges[:3]), 7)
}

func TestTakeToRepair_PrefersTheTakeTheShotUsesThenTheAIPickThenTheNewest(t *testing.T) {
	take := func(id string, status TakeStatus, adopted bool, file string) TakeInfo {
		return TakeInfo{Take: Take{ID: id, Status: status, FilePath: file}, Adopted: adopted}
	}
	takes := []TakeInfo{
		take("a", TakeCandidate, false, "a.mp4"),
		take("b", TakeAIPick, false, "b.mp4"),
		take("c", TakeCandidate, false, "c.mp4"),
		take("d", TakeSelected, true, "d.mp4"),
		take("e", TakeCandidate, false, ""),
	}

	id, ok := TakeToRepair(takes)
	require.True(t, ok)
	assert.Equal(t, "d", id)

	id, _ = TakeToRepair(takes[:3])
	assert.Equal(t, "b", id)

	id, _ = TakeToRepair([]TakeInfo{takes[0], takes[2], take("r", TakeRejected, false, "r.mp4")})
	assert.Equal(t, "c", id, "rejected Takes and Takes without a file do not count")

	_, ok = TakeToRepair([]TakeInfo{takes[4]})
	assert.False(t, ok)
}

func TestFormatTimeRanges(t *testing.T) {
	assert.Equal(t, "2.1-2.8秒, 4.0-4.5秒", FormatTimeRanges([]TimeRange{{2.1, 2.8}, {4, 4.5}}))
}
