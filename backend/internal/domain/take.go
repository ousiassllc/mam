package domain

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// TakeScores are the four scores the Take comparison shows (spec/spec.md §14), each 0-100. They
// are taken from the Issues of the latest AI Video Judge result: a Take starts with 100 on every
// axis and each Issue subtracts a penalty by its severity from the axis of its dimension.
type TakeScores struct {
	Product    float64 `json:"product"`
	Acting     float64 `json:"acting"`
	Camera     float64 `json:"camera"`
	Continuity float64 `json:"continuity"`
}

// Overall is the mean of the four scores.
func (s TakeScores) Overall() float64 {
	return (s.Product + s.Acting + s.Camera + s.Continuity) / 4
}

// issuePenalty is what an Issue of the severity takes off the score of its axis.
func issuePenalty(s IssueSeverity) float64 {
	switch s {
	case SeverityHigh:
		return 35
	case SeverityMedium:
		return 15
	case SeverityLow:
		return 5
	}
	return 0
}

// scoreAxis is the comparison axis a Judge dimension counts for; dimensions that are not listed
// (prompt adherence, for one) leave the four scores alone.
func scoreAxis(d JudgeDimension) (axis string, ok bool) {
	switch d {
	case DimProductAccuracy, DimProductGeometry, DimLogoTextAccuracy, DimObjectConsistency:
		return "product", true
	case DimCharacterIdentity, DimCharacterConsistency, DimFace, DimHands, DimPose, DimActing, DimLipSync, DimMotionQuality:
		return "acting", true
	case DimCameraMotion, DimComposition, DimLighting:
		return "camera", true
	case DimSceneContinuity, DimBackground, DimTemporal, DimFlicker, DimShotCompatibility, DimPhysics:
		return "continuity", true
	}
	return "", false
}

// ScoreTake derives the four scores of a Take from the Issues of its Judge result. A
// face_identity_drift Issue counts for Acting whatever dimension it names.
func ScoreTake(issues []JudgeIssue) TakeScores {
	penalty := map[string]float64{}
	for _, is := range issues {
		axis, ok := scoreAxis(is.Dimension)
		if is.Type == IssueTypeFaceIdentityDrift {
			axis, ok = "acting", true
		}
		if ok {
			penalty[axis] += issuePenalty(is.Severity)
		}
	}
	score := func(axis string) float64 { return max(100-penalty[axis], 0) }
	return TakeScores{Product: score("product"), Acting: score("acting"), Camera: score("camera"), Continuity: score("continuity")}
}

// Usable tells whether the Take can still be acted on: it is not rejected and its file was not
// archived. It is the rule behind every Take action (select, reject, Partial Take Use), in the API,
// in the pages and in the repository that refuses what it does not allow.
func (t Take) Usable() bool { return t.Status != TakeRejected && t.FilePath != "" }

// Selectable tells whether the Take can be selected: it is usable and not already the selected one.
func (t Take) Selectable() bool { return t.Usable() && t.Status != TakeSelected }

// TakeInfo is a Take with what the comparison of the Takes of a Shot shows.
type TakeInfo struct {
	Take
	// Scores is nil until the Take has been judged.
	Scores *TakeScores
	// Verdict is the verdict of the latest Judge result ("" until the Take has been judged).
	Verdict JudgeVerdict
	// AssetID is the video asset of the Take ("" when the file was archived).
	AssetID string
	// Resolution is what the Model Adapter reported ("1080p"); DurationSeconds is the length it made.
	Resolution      string
	DurationSeconds float64
	// Adopted is true for the Take the Shot uses (shot.selected_take_id).
	Adopted bool
}

// ResolutionHeight is the number of lines of "1080p", "2160p" or "4K" (0 when unknown).
func ResolutionHeight(res string) int {
	res = strings.ToLower(strings.TrimSpace(res))
	switch res {
	case "4k", "uhd":
		return 2160
	case "8k":
		return 4320
	}
	n, err := strconv.Atoi(strings.TrimSuffix(res, "p"))
	if err != nil {
		return 0
	}
	return n
}

// Proxied is true when the preview plays a proxy instead of the file: the picture is taller than
// 1080 lines (spec/requirements/non-functional.md §2).
func (t TakeInfo) Proxied() bool { return ResolutionHeight(t.Resolution) > 1080 }

// PickTake is the AI Pick of the Takes of a Shot: among the Takes that were judged and not
// rejected, the best one — a Take that passed the Judge before one that failed, then by the mean of
// the four scores, then the older Take. takes are oldest first. ok is false when no Take qualifies;
// passed tells whether the picked Take passed the Judge.
func PickTake(takes []TakeInfo) (id string, passed, ok bool) {
	var best *TakeInfo
	for i := range takes {
		t := &takes[i]
		if !pickable(*t) {
			continue
		}
		if best == nil || betterTake(*t, *best) {
			best = t
		}
	}
	if best == nil {
		return "", false, false
	}
	return best.ID, best.Verdict == JudgePass, true
}

// pickable is true for a Take the AI Pick may choose: judged and not rejected.
func pickable(t TakeInfo) bool { return t.Status != TakeRejected && t.Scores != nil }

// PickAcceptedTake is PickTake restricted by the Retry Policy: accepted tells whether a Take meets
// it, and when some Take that could be picked does, the pick is the best of those. A Take the user
// rejected counts for neither (it can neither be picked nor make the others fall short of the
// policy). A nil accepted accepts every Take.
func PickAcceptedTake(takes []TakeInfo, accepted func(takeID string) bool) (id string, passed, ok bool) {
	pool := slices.DeleteFunc(slices.Clone(takes), func(t TakeInfo) bool { return !pickable(t) })
	if accepted != nil {
		if meets := slices.DeleteFunc(slices.Clone(pool), func(t TakeInfo) bool { return !accepted(t.ID) }); len(meets) > 0 {
			pool = meets
		}
	}
	return PickTake(pool)
}

func betterTake(a, b TakeInfo) bool {
	if pa, pb := a.Verdict == JudgePass, b.Verdict == JudgePass; pa != pb {
		return pa
	}
	return a.Scores.Overall() > b.Scores.Overall()
}

// TakeSegment is the part of a Take a Partial Take Use adopts: Start to End seconds of the Take.
type TakeSegment struct {
	TakeID string  `json:"take_id"`
	Start  float64 `json:"start"`
	End    float64 `json:"end"`
}

// Partial Take Use limits.
const (
	// MaxTakeSegments bounds the number of segments of one composition.
	MaxTakeSegments = 8
	// MinTakeSegmentSeconds is the shortest segment: a cross-fade needs something to fade.
	MinTakeSegmentSeconds = 0.3
)

// PartialTakeKind is the "kind" of the model parameters of a Take composed from segments of others.
const PartialTakeKind = "partial_take"

// TakeLabel is how the comparison names the n-th Take of a Shot (0-based): "A", "B", ... "Z", then "27", "28", ...
func TakeLabel(n int) string {
	if n >= 0 && n < 26 {
		return string(rune('A' + n))
	}
	return strconv.Itoa(n + 1)
}

// segmentPattern is one "Take A の 0-2秒" of a natural-language instruction: a Take label (a
// letter or a number), then the start and end of the part in seconds.
var segmentPattern = regexp.MustCompile(
	`(?i)take\s*([a-z]|\d+)\s*(?:の|:|：|@|,|、)?\s*(\d+(?:\.\d+)?)\s*(?:秒|s|sec)?\s*(?:-|–|—|~|〜|～|から|to)\s*(\d+(?:\.\d+)?)\s*(?:秒|s|sec)?`)

// ParseTakeSegments reads a Partial Take Use written in words, such as "Take A の 0-2秒 + Take B の
// 2-4秒", against the Takes of the Shot (oldest first: Take A is the first). Segments keep the order of
// the text, which is the order they are joined in.
func ParseTakeSegments(text string, takes []TakeInfo) ([]TakeSegment, error) {
	found := segmentPattern.FindAllStringSubmatch(text, -1)
	if len(found) == 0 {
		return nil, errors.New("区間を読み取れませんでした。「Take A の 0-2秒 + Take B の 2-4秒」のように指定してください")
	}
	segs := make([]TakeSegment, 0, len(found))
	for _, m := range found {
		var idx int
		if n, err := strconv.Atoi(m[1]); err == nil {
			idx = n - 1
		} else {
			idx = int(strings.ToUpper(m[1])[0] - 'A')
		}
		if idx < 0 || idx >= len(takes) {
			return nil, fmt.Errorf("Take %s はこの Shot にありません", strings.ToUpper(m[1]))
		}
		start, _ := strconv.ParseFloat(m[2], 64)
		end, _ := strconv.ParseFloat(m[3], 64)
		segs = append(segs, TakeSegment{TakeID: takes[idx].ID, Start: start, End: end})
	}
	return segs, nil
}

// ValidateTakeSegments checks a Partial Take Use against the Takes of the Shot: every Take must
// belong to the Shot, still have its file and not be rejected, and every part must lie inside the
// Take (when the Take's length is known) and be at least MinTakeSegmentSeconds long.
func ValidateTakeSegments(segs []TakeSegment, takes []TakeInfo) error {
	if len(segs) < 2 {
		return errors.New("部分採用には 2 つ以上の区間を指定してください（Take 全体を採用するときは選択してください）")
	}
	if len(segs) > MaxTakeSegments {
		return fmt.Errorf("区間は最大 %d 個までです", MaxTakeSegments)
	}
	for i, s := range segs {
		n := i + 1
		var take *TakeInfo
		for j := range takes {
			if takes[j].ID == s.TakeID {
				take = &takes[j]
			}
		}
		switch {
		case take == nil:
			return fmt.Errorf("区間 %d の Take がこの Shot にありません", n)
		case take.Status == TakeRejected:
			return fmt.Errorf("区間 %d の Take は除外されています", n)
		case take.FilePath == "":
			return fmt.Errorf("区間 %d の Take のファイルはアーカイブされています", n)
		case s.Start < 0 || s.End-s.Start < MinTakeSegmentSeconds:
			return fmt.Errorf("区間 %d は %.1f 秒以上にしてください", n, MinTakeSegmentSeconds)
		case take.DurationSeconds > 0 && s.End > take.DurationSeconds+0.001:
			return fmt.Errorf("区間 %d は Take の長さ（%.1f 秒）を超えています", n, take.DurationSeconds)
		}
	}
	return nil
}

// TakeVideoURL is the same-origin URL of the preview video of a Take: the file itself, or a 1080p
// proxy of a 4K one.
func TakeVideoURL(takeID string) string { return "/takes/" + url.PathEscape(takeID) + "/video" }

// TakeThumbnailURL is the same-origin URL of a still of the first frame of a Take.
func TakeThumbnailURL(takeID string) string { return "/takes/" + url.PathEscape(takeID) + "/thumbnail" }
