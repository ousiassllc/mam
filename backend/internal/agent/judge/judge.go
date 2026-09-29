// Package judge is the AI Video Judge (spec/spec.md §12): it watches a generated Take and decides
// whether it can be used. It returns time- and frame-level Issues and a Repair Strategy, not just
// a score. Two evaluations are combined:
//
//   - the qualitative evaluation, in which the Reasoning LLM (AgentVideoJudge, Gemini by default)
//     is shown sampled frames and the Reference Pack and rates the evaluation dimensions;
//   - the Face Identity Similarity, a numeric measurement that runs locally (§12.4).
//
// The verdict is decided by deterministic rules (see Decide), never by the model alone: a LOCK
// violation fails the Take whatever the model says.
package judge

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/planning"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/facesim"
)

// ErrInvalidJudgement means the Reasoning LLM answered, but not with a usable Judge result. It is
// an unusable AI output like planning.ErrInvalidPlan, which it wraps.
var ErrInvalidJudgement = fmt.Errorf("judge: %w", planning.ErrInvalidPlan)

// Judge is the AI Video Judge. It is safe for concurrent use.
type Judge struct {
	llm    llm.Completer
	frames FrameSource
	faces  *FaceMeasurer // nil: the numeric measurement is not available
	cfg    Config
	logger *slog.Logger
}

// New returns a Judge. engine may be nil: Takes are then judged qualitatively only and the result
// says so (metrics.face_identity_status = "unavailable").
func New(c llm.Completer, frames FrameSource, engine facesim.Engine, cfg Config, logger *slog.Logger) *Judge {
	j := &Judge{llm: c, frames: frames, cfg: cfg, logger: logger}
	if engine != nil {
		j.faces = NewFaceMeasurer(engine, cfg.Face)
	}
	if j.logger == nil {
		j.logger = slog.New(slog.DiscardHandler)
	}
	return j
}

// RefImage is a Reference Pack image shown to the Reasoning LLM next to the frames.
type RefImage struct {
	Kind     domain.ReferenceKind
	Name     string
	MimeType string
	Data     []byte
}

// Input is what the Judge works from.
type Input struct {
	// VideoPath is the file of the Take.
	VideoPath string
	// Spec is the Canonical Shot Spec the Take was generated from; Previous and Next are the
	// neighbouring Shots' specs (nil when there is none), for Previous / Next Shot Compatibility.
	Spec           domain.ShotSpec
	Previous, Next *domain.ShotSpec
	// References are shown to the Reasoning LLM (identity, product and look).
	References []RefImage
	// FaceSubjects are the Characters whose face similarity is measured, with their reference images.
	FaceSubjects []FaceSubject
	// Locked are the LOCKed dimensions: any Issue of at least medium severity in one of them is a
	// LOCK violation and fails the Take.
	Locked map[domain.JudgeDimension]bool
}

// Evaluate judges a Take. The returned result has no ID, TakeID or CreatedAt yet.
func (j *Judge) Evaluate(ctx context.Context, in Input) (domain.JudgeResult, error) {
	review, err := j.frames.Sample(ctx, in.VideoPath, SampleSpec{
		FPS: j.cfg.Frames.FaceSampleFPS, MaxFrames: j.cfg.Frames.ReviewFrames, MaxWidth: j.cfg.Frames.ReviewMaxWidth,
	})
	if err != nil {
		return domain.JudgeResult{}, fmt.Errorf("sample review frames: %w", err)
	}

	metrics, faceIssues := j.measureFaces(ctx, in)
	if err := ctx.Err(); err != nil {
		return domain.JudgeResult{}, err
	}

	ans, err := llm.CompleteJSON[answer](ctx, j.llm, llm.AgentVideoJudge, j.request(in, review, metrics))
	if err != nil {
		return domain.JudgeResult{}, err
	}
	result, err := ans.result(review, in.Locked, hasEvaluated(metrics))
	if err != nil {
		return domain.JudgeResult{}, err
	}
	for i := range faceIssues {
		if in.Locked[domain.DimCharacterIdentity] && faceIssues[i].Severity == domain.SeverityHigh {
			faceIssues[i].LockViolation = true
		}
	}
	result.Issues = append(result.Issues, faceIssues...)
	result.Metrics = metrics
	Decide(&result, ans.verdict())
	return result, nil
}

// hasEvaluated reports whether the numeric face measurement produced numbers that count.
func hasEvaluated(m domain.JudgeMetrics) bool {
	for _, f := range m.FaceIdentity {
		if f.Evaluated {
			return true
		}
	}
	return false
}

// measureFaces runs the Face Identity Similarity. It never fails the Judge: without the models, or
// when the measurement breaks, the Take is judged qualitatively and the status says why.
func (j *Judge) measureFaces(ctx context.Context, in Input) (domain.JudgeMetrics, []domain.JudgeIssue) {
	if len(in.FaceSubjects) == 0 {
		return domain.JudgeMetrics{FaceIdentityStatus: domain.FaceNotApplicable}, nil
	}
	unavailable := domain.JudgeMetrics{FaceIdentityStatus: domain.FaceUnavailable}
	if j.faces == nil {
		return unavailable, nil
	}
	clip, err := j.frames.Sample(ctx, in.VideoPath, SampleSpec{
		FPS: j.cfg.Frames.FaceSampleFPS, MaxFrames: j.cfg.Frames.MaxFaceFrames, MaxWidth: j.cfg.Frames.FaceMaxWidth,
	})
	if err == nil {
		var rep FaceReport
		if rep, err = j.faces.Measure(ctx, in.FaceSubjects, clip); err == nil {
			if len(rep.Metrics) == 0 {
				return domain.JudgeMetrics{FaceIdentityStatus: domain.FaceNotApplicable}, nil
			}
			return domain.JudgeMetrics{FaceIdentity: rep.Metrics, FaceIdentityStatus: domain.FaceMeasured}, rep.Issues
		}
	}
	if ctx.Err() == nil {
		j.logger.Warn("face identity measurement failed; judging qualitatively only", "err", err)
	}
	return unavailable, nil
}

// answer is the JSON the Reasoning LLM returns.
type answer struct {
	Result            string        `json:"result"`
	OverallConfidence *float64      `json:"overall_confidence"`
	Issues            []answerIssue `json:"issues"`
	Preserve          []string      `json:"preserve"`
	RepairStrategy    *answerRepair `json:"repair_strategy"`
}

type answerIssue struct {
	Type        string `json:"type"`
	Severity    string `json:"severity"`
	Dimension   string `json:"dimension"`
	TimeRange   string `json:"time_range"`
	CharacterID string `json:"character_id"`
	Description string `json:"description"`
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
// measurement produced the authoritative ones.
func (a answer) result(clip Clip, locked map[domain.JudgeDimension]bool, skipFaceDrift bool) (domain.JudgeResult, error) {
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
		res.Issues = append(res.Issues, issue)
	}
	if rs := a.RepairStrategy; rs != nil {
		action := domain.RepairAction(identifier(rs.Action))
		if action.Valid() {
			res.RepairStrategy = &domain.RepairStrategy{Action: action, Instructions: cleanIdentifiers(rs.Instructions)}
		}
	}
	return res, nil
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

// request builds the model call: instructions, the spec, then reference images and frames.
func (j *Judge) request(in Input, clip Clip, metrics domain.JudgeMetrics) llm.Request {
	req := llm.Request{System: systemPrompt, User: userPrompt(in, clip, metrics)}
	refs := in.References
	if n := j.cfg.Frames.MaxReferenceImages; len(refs) > n {
		refs = refs[:n]
	}
	for _, r := range refs {
		caption := "Reference image (" + string(r.Kind) + ")"
		if r.Name != "" {
			caption += ": " + r.Name
		}
		req.Images = append(req.Images, llm.Image{MimeType: r.MimeType, Data: r.Data, Caption: caption})
	}
	for i, f := range evenly(clip.Frames, j.cfg.Frames.ReviewFrames) {
		req.Images = append(req.Images, llm.Image{
			MimeType: "image/jpeg", Data: f.JPEG,
			Caption: fmt.Sprintf("Video frame %d of the Take at t=%.2fs (source frame %d)", i+1, f.Time, f.Number),
		})
	}
	return req
}

func userPrompt(in Input, clip Clip, metrics domain.JudgeMetrics) string {
	var b strings.Builder
	spec, _ := json.MarshalIndent(in.Spec, "", "  ")
	b.WriteString("Judge the generated video Take of this shot.\n\nCANONICAL SHOT SPEC (what the shot must show):\n")
	b.Write(spec)
	for _, n := range []struct {
		label string
		spec  *domain.ShotSpec
	}{{"PREVIOUS SHOT SPEC (the Take must connect to it)", in.Previous}, {"NEXT SHOT SPEC (the Take must connect to it)", in.Next}} {
		if n.spec != nil {
			raw, _ := json.Marshal(n.spec)
			fmt.Fprintf(&b, "\n\n%s:\n%s", n.label, raw)
		}
	}
	if locked := lockedNames(in.Locked); len(locked) > 0 {
		fmt.Fprintf(&b, "\n\nLOCKED dimensions (the user forbids any change; any visible deviation is a lock violation): %s", strings.Join(locked, ", "))
	}
	fmt.Fprintf(&b, "\n\nThe Take is %.1f seconds long at %.1f fps. The frames below are sampled evenly; times are seconds from the start.", clip.Duration, clip.FPS)
	if hasEvaluated(metrics) {
		b.WriteString("\nThe face identity of the Character is measured separately by a numeric model: do NOT report face_identity_drift yourself; still judge the face qualitatively (naturalness, distortion, expression).")
	}
	b.WriteString("\nReference images (if any) come first, then the video frames.")
	return b.String()
}

func lockedNames(locked map[domain.JudgeDimension]bool) []string {
	var out []string
	for _, d := range domain.JudgeDimensions() {
		if locked[d] {
			out = append(out, string(d))
		}
	}
	return out
}

var systemPrompt = buildSystemPrompt()

func buildSystemPrompt() string {
	dims := make([]string, 0, len(domain.JudgeDimensions()))
	for _, d := range domain.JudgeDimensions() {
		dims = append(dims, string(d))
	}
	actions := make([]string, 0, len(domain.RepairActions()))
	for _, a := range domain.RepairActions() {
		actions = append(actions, string(a))
	}
	return `You are the AI Video Judge of an AI video production team.
You are shown frames of ONE generated video Take, sometimes reference images, and the shot's Canonical Shot Spec.
Decide whether the Take can be used. Do not give only a score: point at the time ranges that are wrong and say how to repair them.

Evaluate these dimensions (use these exact identifiers in "dimension"): ` + strings.Join(dims, ", ") + `.

Return ONLY one JSON object:
{
  "result": "pass" or "fail",
  "overall_confidence": a number from 0 to 1 (how sure you are of the result),
  "issues": [
    {"type": "snake_case_identifier such as product_logo_deformation or camera_speed",
     "severity": "low" | "medium" | "high",
     "dimension": "one of the dimension identifiers",
     "time_range": "2.1-2.8s" (seconds from the start of the Take; omit if it concerns the whole Take),
     "description": "one short English sentence"}
  ],
  "preserve": ["what is good and must be kept when repairing, e.g. character, acting, background"],
  "repair_strategy": {"action": one of ` + strings.Join(actions, ", ") + `,
                      "instructions": ["snake_case_instruction such as reduce_camera_speed_40_percent"]}
}

Rules:
- severity "high" means the Take cannot be used as it is; "medium" is a visible flaw; "low" is minor.
- If a LOCKED dimension is listed, any visible deviation of it is at least "medium".
- Report only what you can see in the frames; do not invent issues. An empty "issues" list is fine for a good Take.
- Use "partial_regeneration" when only some time ranges are wrong and "full_retake" when the whole Take is wrong.
- Write English only, except for names copied from the input.`
}
