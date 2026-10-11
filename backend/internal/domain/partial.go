package domain

import (
	"fmt"
	"slices"
	"strings"
)

// TimeRange is a stretch of a Take in seconds (From < To).
type TimeRange struct {
	From float64 `json:"from"`
	To   float64 `json:"to"`
}

// SpliceMarginSeconds is how much of the picture around a problem range is regenerated too, so that
// the cross-fade of the joint (media.FadeSeconds) happens on new material and not on the problem.
const SpliceMarginSeconds = 0.2

// IssueRanges are the time ranges of the Issues of a Judge result. ok is false when the Issues do
// not all point at a time range (or there is none): the problem is then not local to a stretch of the
// Shot, and only a whole new Take can fix it.
func IssueRanges(issues []JudgeIssue) (ranges []TimeRange, ok bool) {
	if len(issues) == 0 {
		return nil, false
	}
	for _, is := range issues {
		from, to, err := ParseTimeRange(is.TimeRange)
		if err != nil || from < 0 || to <= from {
			return nil, false
		}
		ranges = append(ranges, TimeRange{From: from, To: to})
	}
	return ranges, true
}

// ParseTimeRanges reads ranges written as "2.1-2.8s" (see ParseTimeRange). It is an error when
// there is none or one of them is not a range inside the Shot's duration.
func ParseTimeRanges(texts []string, duration float64) ([]TimeRange, error) {
	var ranges []TimeRange
	for _, t := range texts {
		from, to, err := ParseTimeRange(t)
		switch {
		case err != nil:
			return nil, fmt.Errorf("時間範囲 %q を読み取れません（「2.1-2.8」のように秒で指定してください）", strings.TrimSpace(t))
		case from < 0 || shorterThan(to-from, MinTakeSegmentSeconds):
			return nil, fmt.Errorf("時間範囲 %q は %.1f 秒以上の区間にしてください", strings.TrimSpace(t), MinTakeSegmentSeconds)
		case duration > 0 && from >= duration:
			return nil, fmt.Errorf("時間範囲 %q は Shot の長さ（%.1f 秒）を超えています", strings.TrimSpace(t), duration)
		}
		ranges = append(ranges, TimeRange{From: from, To: to})
	}
	if len(ranges) == 0 {
		return nil, fmt.Errorf("時間範囲を指定してください")
	}
	return ranges, nil
}

// SpliceSegments lays out a Partial Regeneration for Partial Take Use: the picture of the base Take
// everywhere but in the ranges, where it is the picture of the regenerated Take. The two Takes are
// taken to run on the same clock (both are the same Shot). The ranges are widened by
// SpliceMarginSeconds, made at least MinTakeSegmentSeconds long and merged when they are too close
// for the base Take to show between them; a base part shorter than MinTakeSegmentSeconds at either
// end is given to the regenerated Take. baseDuration and regeneratedDuration are the lengths of the
// files (0 when unknown); the shorter one bounds the result.
//
// It returns nil when no splice is worth making: the ranges leave nothing of the base Take (the new
// Take replaces it as a whole), the Takes are too short, or more than MaxTakeSegments would be needed.
func SpliceSegments(baseID string, baseDuration float64, regeneratedID string, regeneratedDuration float64, ranges []TimeRange) []TakeSegment {
	duration := baseDuration
	if duration <= 0 || (regeneratedDuration > 0 && regeneratedDuration < duration) {
		duration = regeneratedDuration
	}
	if duration < 2*MinTakeSegmentSeconds {
		return nil
	}

	var padded []TimeRange
	for _, r := range ranges {
		from, to := max(r.From-SpliceMarginSeconds, 0), min(r.To+SpliceMarginSeconds, duration)
		if from >= duration || to <= 0 {
			continue
		}
		if to-from < MinTakeSegmentSeconds {
			to = min(from+MinTakeSegmentSeconds, duration)
			from = max(to-MinTakeSegmentSeconds, 0)
		}
		padded = append(padded, TimeRange{From: from, To: to})
	}
	slices.SortFunc(padded, func(a, b TimeRange) int {
		switch {
		case a.From < b.From:
			return -1
		case a.From > b.From:
			return 1
		}
		return 0
	})
	var merged []TimeRange
	for _, r := range padded {
		if n := len(merged); n > 0 && r.From-merged[n-1].To < MinTakeSegmentSeconds {
			merged[n-1].To = max(merged[n-1].To, r.To)
			continue
		}
		merged = append(merged, r)
	}

	var segs []TakeSegment
	cursor := 0.0
	for _, r := range merged {
		if r.From-cursor >= MinTakeSegmentSeconds {
			segs = append(segs, TakeSegment{TakeID: baseID, Start: cursor, End: r.From})
		} else {
			r.From = cursor // too little of the base Take to show: the new one starts at the cursor
		}
		segs = append(segs, TakeSegment{TakeID: regeneratedID, Start: r.From, End: r.To})
		cursor = r.To
	}
	switch {
	case len(merged) == 0:
		return nil
	case duration-cursor >= MinTakeSegmentSeconds:
		segs = append(segs, TakeSegment{TakeID: baseID, Start: cursor, End: duration})
	default:
		segs[len(segs)-1].End = duration
	}
	if len(segs) < 2 || len(segs) > MaxTakeSegments {
		return nil
	}
	return segs
}

// FormatTimeRanges is "2.1-2.8秒, 4.0-4.5秒".
func FormatTimeRanges(ranges []TimeRange) string {
	texts := make([]string, len(ranges))
	for i, r := range ranges {
		texts[i] = fmt.Sprintf("%.1f-%.1f秒", r.From, r.To)
	}
	return strings.Join(texts, ", ")
}

// TakeToRepair is the Take a Partial Regeneration the director asks for starts from, given the
// Takes of the Shot oldest first: the Take the Shot uses, else the AI Pick, else the newest Take
// that is not rejected. Only Takes with a file count.
func TakeToRepair(takes []TakeInfo) (string, bool) {
	usable := slices.DeleteFunc(slices.Clone(takes), func(t TakeInfo) bool { return t.FilePath == "" || t.Status == TakeRejected })
	if i := slices.IndexFunc(usable, func(t TakeInfo) bool { return t.Adopted }); i >= 0 {
		return usable[i].ID, true
	}
	if i := slices.IndexFunc(usable, func(t TakeInfo) bool { return t.Status == TakeAIPick }); i >= 0 {
		return usable[i].ID, true
	}
	if len(usable) == 0 {
		return "", false
	}
	return usable[len(usable)-1].ID, true
}
