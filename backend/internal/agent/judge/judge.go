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
	"fmt"
	"log/slog"

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
	// Template is the Production Template of the Project. When set, the Judge scores its evaluation
	// axes (0-100), which the Retry Policy's Minimum acceptance score is applied to.
	Template domain.ProductionTemplate
	// Brand are the Safety / Brand Constraints of the Project (F-23). The Reasoning LLM is asked to
	// check the Take against them; a broken LOCK constraint fails the Take whatever the score is,
	// a broken Advisory one is only a low severity warning.
	Brand domain.BrandConstraints
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
	result, err := ans.result(review, in, hasEvaluated(metrics))
	if err != nil {
		return domain.JudgeResult{}, err
	}
	for i := range faceIssues {
		if in.Locked[domain.DimCharacterIdentity] && faceIssues[i].Severity == domain.SeverityHigh {
			faceIssues[i].LockViolation = true
		}
	}
	result.Issues = append(result.Issues, faceIssues...)
	result.Issues = append(result.Issues, specBrandIssues(in)...)
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
	if j.faces == nil || !facesim.Usable(j.faces.engine) { // the models can be installed while mam runs (#1026): asked every time
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
