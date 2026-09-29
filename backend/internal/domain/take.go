package domain

import (
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
		if t.Status == TakeRejected || t.Scores == nil {
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
