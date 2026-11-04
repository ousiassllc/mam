package judge

import (
	"fmt"
	"math"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// answer is the JSON the Reasoning LLM returns.
type answer struct {
	Result            string        `json:"result"`
	OverallConfidence *float64      `json:"overall_confidence"`
	Issues            []answerIssue `json:"issues"`
	Preserve          []string      `json:"preserve"`
	RepairStrategy    *answerRepair `json:"repair_strategy"`
	// Scores are 0-100 per axis of the Production Template (optional). Values are kept untyped so that
	// a malformed score is dropped instead of invalidating the whole judgement.
	Scores map[string]any `json:"scores"`
}

type answerIssue struct {
	Type        string `json:"type"`
	Severity    string `json:"severity"`
	Dimension   string `json:"dimension"`
	TimeRange   string `json:"time_range"`
	CharacterID string `json:"character_id"`
	Description string `json:"description"`
	// BrandConstraintID is the id of the Brand / Safety Constraint the Issue reports as broken.
	BrandConstraintID string `json:"brand_constraint_id"`
}

type answerRepair struct {
	Action       string   `json:"action"`
	Instructions []string `json:"instructions"`
}

func (a answer) verdict() domain.JudgeVerdict {
	return domain.JudgeVerdict(strings.ToLower(strings.TrimSpace(a.Result)))
}

// result validates the answer and converts it: issue types become snake_case identifiers, time
// ranges are checked against the clip and given source frame numbers, unknown dimensions are
// dropped. skipFaceDrift drops the model's own face_identity_drift Issues when the numeric
// measurement produced the authoritative ones. Scores of axes outside the Production Template, or
// outside 0-100, are dropped. An Issue that names a Brand constraint is a violation of it: of a LOCK
// constraint it fails the Take, of an Advisory one it stays a low severity warning.
func (a answer) result(clip Clip, in Input, skipFaceDrift bool) (domain.JudgeResult, error) {
	locked, template := in.Locked, in.Template
	if v := a.verdict(); v != domain.JudgePass && v != domain.JudgeFail {
		return domain.JudgeResult{}, fmt.Errorf("%w: result %q is neither pass nor fail", ErrInvalidJudgement, a.Result)
	}
	if a.OverallConfidence == nil || *a.OverallConfidence < 0 || *a.OverallConfidence > 1 {
		return domain.JudgeResult{}, fmt.Errorf("%w: overall_confidence must be a number in 0..1", ErrInvalidJudgement)
	}
	res := domain.JudgeResult{OverallConfidence: *a.OverallConfidence, Preserve: cleanIdentifiers(a.Preserve)}
	for i, ai := range a.Issues {
		typ := identifier(ai.Type)
		if typ == "" {
			return domain.JudgeResult{}, fmt.Errorf("%w: issues[%d] has no type", ErrInvalidJudgement, i)
		}
		sev := domain.IssueSeverity(strings.ToLower(strings.TrimSpace(ai.Severity)))
		if !sev.Valid() {
			return domain.JudgeResult{}, fmt.Errorf("%w: issues[%d] severity %q is not low, medium or high", ErrInvalidJudgement, i, ai.Severity)
		}
		if skipFaceDrift && typ == domain.IssueTypeFaceIdentityDrift {
			continue
		}
		issue := domain.JudgeIssue{Type: typ, Severity: sev, CharacterID: ai.CharacterID, Description: strings.TrimSpace(ai.Description)}
		if d := domain.JudgeDimension(identifier(ai.Dimension)); d.Valid() {
			issue.Dimension = d
		}
		if from, to, err := domain.ParseTimeRange(ai.TimeRange); err == nil && to > from && from < clip.Duration {
			to = min(to, clip.Duration)
			issue.TimeRange = FormatTimeRange(from, to)
			first, last := clip.FrameRange(from, to)
			issue.Frames = []int{first, last}
		}
		issue.LockViolation = issue.Dimension != "" && locked[issue.Dimension] && sev.Rank() >= domain.SeverityMedium.Rank()
		if c, ok := in.Brand.ByID(strings.TrimSpace(ai.BrandConstraintID)); ok {
			applyBrand(&issue, c)
		}
		res.Issues = append(res.Issues, issue)
	}
	res.Scores = a.axisScores(template)
	if rs := a.RepairStrategy; rs != nil {
		action := domain.RepairAction(identifier(rs.Action))
		if action.Valid() {
			res.RepairStrategy = &domain.RepairStrategy{Action: action, Instructions: cleanIdentifiers(rs.Instructions)}
		}
	}
	return res, nil
}

// applyBrand makes the Issue a violation of the Brand constraint c: a LOCK constraint fails the
// Take whatever the severity or the score is; an Advisory one is a warning that cannot fail the Take.
func applyBrand(issue *domain.JudgeIssue, c domain.BrandConstraint) {
	issue.Type, issue.BrandConstraintID = domain.BrandIssueType, c.ID
	if c.Level == domain.EnforceLock {
		issue.LockViolation = true
		issue.Severity = domain.SeverityHigh
		return
	}
	issue.LockViolation = false
	issue.Severity = domain.SeverityLow
}

// specBrandIssues are the forbidden expressions of the Project's Brand constraints found in the
// Canonical Shot Spec the Take was made from. This check is deterministic: it does not depend on
// what the Reasoning LLM notices, and it catches constraints registered after the spec was written.
func specBrandIssues(in Input) []domain.JudgeIssue {
	var out []domain.JudgeIssue
	for _, v := range in.Brand.ForbiddenIn(in.Spec.BrandText()) {
		is := domain.JudgeIssue{Description: v.Message()}
		applyBrand(&is, v.Constraint)
		out = append(out, is)
	}
	return out
}

// axisScores keeps the scores of the axes of the template that are numbers in 0-100.
func (a answer) axisScores(template domain.ProductionTemplate) map[domain.EvaluationAxis]float64 {
	var out map[domain.EvaluationAxis]float64
	for _, axis := range template.Axes() {
		s, ok := a.Scores[string(axis)].(float64)
		if !ok || math.IsNaN(s) || s < 0 || s > domain.MaxScore {
			continue
		}
		if out == nil {
			out = map[domain.EvaluationAxis]float64{}
		}
		out[axis] = s
	}
	return out
}

// Decide sets the verdict and completes the Repair Strategy of the result. The rules are
// deterministic, so a verdict can be explained:
//
//  1. A LOCK violation fails the Take whatever the model's verdict or confidence is (§13).
//  2. Otherwise the Take fails when the model says so or any Issue has high severity.
//  3. Frames without a detectable face are missing values and never make the Take fail; that is
//     already the case because only measured runs of frames produce face_identity_drift Issues.
//
// A face_identity_drift Issue makes Reference Reinforcement the preferred repair (§12.4).
func Decide(res *domain.JudgeResult, model domain.JudgeVerdict) {
	fail := model == domain.JudgeFail
	drift := false
	for _, is := range res.Issues {
		if is.LockViolation || is.Severity == domain.SeverityHigh {
			fail = true
		}
		if is.Type == domain.IssueTypeFaceIdentityDrift && is.Severity == domain.SeverityHigh {
			drift = true
		}
	}
	res.Result = domain.JudgePass
	if fail {
		res.Result = domain.JudgeFail
	}
	if drift {
		if res.RepairStrategy == nil {
			res.RepairStrategy = &domain.RepairStrategy{}
		}
		if a := res.RepairStrategy.Action; a == "" || a == domain.RepairPrompt {
			res.RepairStrategy.Action = domain.RepairReferenceReinforce
		}
		res.RepairStrategy.Instructions = appendUnique(res.RepairStrategy.Instructions, domain.InstructionIncreaseFaceReference)
	}
	if res.RepairStrategy != nil && res.RepairStrategy.Action == "" {
		res.RepairStrategy = nil
	}
}

func appendUnique(list []string, s string) []string {
	for _, v := range list {
		if v == s {
			return list
		}
	}
	return append(list, s)
}

// identifier turns "Product Logo-Deformation" into "product_logo_deformation" ("" when nothing is left).
func identifier(s string) string {
	var b strings.Builder
	underscore := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			if underscore && b.Len() > 0 {
				b.WriteByte('_')
			}
			underscore = false
			b.WriteRune(r)
		} else {
			underscore = true
		}
	}
	return b.String()
}

func cleanIdentifiers(list []string) []string {
	var out []string
	for _, s := range list {
		if id := identifier(s); id != "" {
			out = appendUnique(out, id)
		}
	}
	return out
}
