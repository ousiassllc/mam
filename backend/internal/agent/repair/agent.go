// Package repair is the Repair Agent and the Repair / Retake Loop (spec/spec.md §13, F-14): from the
// Judge result of a failed Take it chooses a repair strategy, makes the new Take within the Retry
// Policy, and asks the user when the policy stops the loop.
//
// The judgements that must be exact are deterministic rules and never go through a model: whether a
// Take is accepted (verdict, LOCK violations = critical constraints, minimum scores), whether the
// Retry Policy allows another try, and which strategies are possible at all. Only the choice among
// the remaining strategies is the Repair Agent's (the Reasoning LLM). TypeSafe AI Jev runs beside
// that choice in shadow mode and is measured, never obeyed (spec/architecture/model-adapters.md §4.5).
package repair

import (
	"context"
	"errors"
	"log/slog"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/agent/jev"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/repair/escalation"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

var (
	// ErrNotJudged means the Take has no Judge result to repair from (409).
	ErrNotJudged = errors.New("repair: the take has not been judged")
	// ErrAccepted means the Take passed the Judge and the Retry Policy's minimum scores: nothing to repair (409).
	ErrAccepted = errors.New("repair: the take is accepted")
	// ErrBusy means the Shot is being generated or repaired (409, no double execution).
	ErrBusy = generation.ErrShotBusy
)

// Chat is what the Repair Agent needs from the chat thread. *service.ChatService implements it.
type Chat interface {
	generation.Chat
	RaiseEscalation(ctx context.Context, projectID, text string, options []string, shotID string) error
}

// Regenerator makes the new Take of a repair. *generation.Orchestrator implements it.
type Regenerator interface {
	RegenerationStrategy(ctx context.Context, ref repository.ShotRef, r generation.Regeneration) (generation.Strategy, error)
	Regenerate(ctx context.Context, ref repository.ShotRef, r generation.Regeneration) (string, error)
}

// Shadow is the Jev client of the shadow run. *jev.Client implements it.
type Shadow interface {
	Choose(ctx context.Context, in jev.Input) (jev.Choice, error)
	Model() string
}

// Splicer joins a Partial Regeneration into the Take it repairs. *service.TakeService implements it.
type Splicer interface {
	// SpliceRegenerated adds a Take that shows baseTakeID outside the ranges and regeneratedTakeID
	// inside them, and returns its ID ("" when no splice is worth making).
	SpliceRegenerated(ctx context.Context, shotID, baseTakeID, regeneratedTakeID string, ranges []domain.TimeRange) (string, error)
}

// TakeJudge has the AI Video Judge look at a Take. *judge.Runner implements it.
type TakeJudge interface {
	Start(ctx context.Context, takeID string) (domain.Job, error)
}

// TakeSelector makes a Take the one its Shot uses. *repository.TakeRepository implements it.
type TakeSelector interface {
	Select(ctx context.Context, takeID string) (string, error)
}

// Deps are the collaborators of an Agent.
type Deps struct {
	Generations *repository.GenerationRepository
	Judges      *repository.JudgeRepository
	Repairs     *repository.RepairRepository
	JobRepo     *repository.JobRepository
	Policies    *repository.PolicyRepository
	Projects    *repository.ProjectRepository
	// Takes, when set, lets an Edit-Level Repair in AUTO mode make the repaired Take the Shot's
	// Selected Take, so that the AUTO editing chain can take it into the Rough Cut.
	Takes       TakeSelector
	Regenerator Regenerator
	Chat        Chat
	Jobs        generation.Jobs
	// LLM is the Reasoning LLM Adapter the Repair Agent reasons through.
	LLM llm.Completer
	// Shadow, when set, is asked the same question as the Repair Agent whenever the Repair Agent
	// chooses. Its answer is only recorded.
	Shadow Shadow
	// Splicer, when set, splices the new Take of a Partial Regeneration into the Take it repairs
	// (it needs ffmpeg). Without it Partial Regeneration only adds the new Take of the whole Shot, and
	// a Partial Regeneration the director asks for by time range is not available.
	Splicer Splicer
	// Judge judges the spliced Take. Required with Splicer.
	Judge TakeJudge
	// Notify receives every progress notification. It MUST NOT block.
	Notify func(generation.Progress)
	Logger *slog.Logger
}

// Agent is the Repair Agent. It is safe for concurrent use.
type Agent struct {
	Deps
	// escalated is what the automatic loop has already asked the user about.
	escalated escalation.Memory
}

// New returns an Agent.
func New(d Deps) *Agent {
	if d.Logger == nil {
		d.Logger = slog.New(slog.DiscardHandler)
	}
	if d.Notify == nil {
		d.Notify = func(generation.Progress) {}
	}
	return &Agent{Deps: d}
}

// payload is the payload of a repair job.
type payload struct {
	TakeID        string `json:"take_id"`
	ShotID        string `json:"shot_id"`
	JudgeResultID string `json:"judge_result_id,omitempty"`
	// Auto is true when the loop started the job by itself after a Judge run (AUTO mode).
	Auto bool `json:"auto,omitempty"`
	// Partial is set when the director asked for a Partial Regeneration (StartPartial): there is no
	// Judge result then, and the Retry Policy does not apply (the user asked).
	Partial *PartialRequest `json:"partial,omitempty"`
}
