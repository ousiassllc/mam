// Package genstage is the generation approval of the AUTO pipeline (spec/spec.md §4.3). After the Scene / Shot plan it asks the user to approve what
// the paid generation will make and cost, and only after the approval does it start the Reference Packs and the Shots, by itself, through the
// Generation Orchestrator. Nothing is started before the approval or after a decline; a change of the plan or a rise of the price asks again;
// every start is claimed in the database first, so that a Shot is never started twice for the same approval, whatever the number of looks
// and restarts. The budget ceilings are not an approval: they go on applying to each start.
//
// The package derives what to do from what is stored (the approval cards, the launches, the Shots), like the stages of the planning pipeline.
package genstage

import (
	"context"
	"errors"
	"log/slog"

	"github.com/ousiassllc/mam/backend/internal/agent/genplan"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/genapprovalrepo"
)

// Chat is what the gate needs from the chat thread. *service.ChatService implements it.
type Chat interface {
	// RequestApproval posts the Approval card of a checkpoint and holds the Project in AWAITING_APPROVAL. It reports whether a card was posted.
	RequestApproval(ctx context.Context, projectID string, cp domain.Checkpoint, summary, refID string) (bool, error)
	// Announce posts an AI message that does not block the Project.
	Announce(ctx context.Context, projectID string, m domain.ChatMessage) error
	// RaiseEscalation asks the user for a decision the AI cannot make (the budget ceiling).
	RaiseEscalation(ctx context.Context, projectID, text string, options []string, shotID string) error
}

// Generator starts the generation of a Shot. *generation.Orchestrator implements it.
type Generator interface {
	StartReferencePack(ctx context.Context, shotID string) (domain.Job, error)
	StartByPolicy(ctx context.Context, shotID, instruction string, preserve []domain.SpecPart) (domain.Job, error)
	UsesCandidates(ctx context.Context, shot domain.Shot) (bool, error)
}

// Deps are the collaborators of a Gate.
type Deps struct {
	Projects *repository.ProjectRepository
	Plans    *repository.PlanRepository
	Gens     *repository.GenerationRepository
	Policies *repository.PolicyRepository
	Jobs     *repository.JobRepository
	Repo     *genapprovalrepo.Repository
	Chat     Chat
	Logger   *slog.Logger
}

// Gate is the generation approval. It holds no per-Project state. Until a Generator is set (SetGenerator) it is off: the Orchestrator stops after
// the Scene / Shot plan, as before, and the user starts each Shot by hand.
type Gate struct {
	d       Deps
	gen     Generator
	builder genplan.Builder
}

// New returns a Gate that is off.
func New(d Deps) *Gate {
	if d.Logger == nil {
		d.Logger = slog.New(slog.DiscardHandler)
	}
	return &Gate{d: d}
}

// SetGenerator turns the gate on. config is the Model Adapter's current setting (the prices). Call it while the application is wired, before the job
// queue starts.
func (g *Gate) SetGenerator(gen Generator, config func() modeladapter.Config) {
	g.gen = gen
	g.builder = genplan.Builder{Plans: g.d.Plans, Gens: g.d.Gens, Policies: g.d.Policies, Config: config, Candidates: gen.UsesCandidates}
}

// On reports whether the gate can start generations.
func (g *Gate) On() bool { return g != nil && g.gen != nil }

// JobChanged looks at the Project of a generation job that changed: when the job is the start of a stage of this gate, kick asks the Orchestrator to
// look at the Project again (the Reference Pack of a Shot that has ended starts its generation). It does nothing for a job started by hand.
func (g *Gate) JobChanged(ctx context.Context, jobID string, kick func(ctx context.Context, projectID string)) {
	if !g.On() {
		return
	}
	job, err := g.d.Jobs.Get(ctx, jobID)
	if err != nil || job.Kind != domain.JobGenerate {
		if err != nil && !errors.Is(err, repository.ErrNotFound) {
			g.d.Logger.Error("read a job for the generation follow-up failed", "job_id", jobID, "err", err)
		}
		return
	}
	shotID, ok := shotOf(job)
	if !ok {
		return
	}
	projectID, ok, err := g.d.Repo.LaunchProject(ctx, shotID)
	switch {
	case err != nil:
		g.d.Logger.Error("find the project of a generation job failed", "job_id", jobID, "err", err)
	case ok:
		kick(ctx, projectID)
	}
}
