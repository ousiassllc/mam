// Package genstage is the automatic start of the generation of the AUTO pipeline (spec/spec.md §4.3). After the Scene / Shot plan it starts the Reference
// Packs and the Shots, by itself, through the Generation Orchestrator: choosing AUTO is the consent, so no approval card is asked for, at any Confirmation
// Level. It announces once what it starts (the models, the estimate with "不明" for a price it does not know, the budget ceilings, how to stop it); every
// start is claimed in the database first, so that a Shot is never started twice, whatever the number of looks and restarts. The budget ceilings
// are not an approval and go on applying to each start: a start one refuses is an Escalation and nothing more is started until the user answers.
//
// The generation approval cards of v0.1.34 (#1108) are the legacy of the policy this replaces: a card that is still pending is settled by the
// Orchestrator (a system answer, not the user's), and a card the user declined keeps the generation from starting by itself until the user writes to the thread or the Scene / Shot plan changes.
//
// The package derives what to do from what is stored (the launches, the Shots, the legacy cards), like the stages of the planning pipeline.
package genstage

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/agent/genplan"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/genapprovalrepo"
)

// Chat is what the gate needs from the chat thread. *service.ChatService implements it.
type Chat interface {
	// Supersede settles a generation approval card of v0.1.34 that is still pending, as the system and not as the user: it posts notice, closes the
	// card and returns the Project from AWAITING_APPROVAL (when no other card holds it). It reports whether it did: false means the card was answered meanwhile.
	Supersede(ctx context.Context, projectID, cardID, notice string) (bool, error)
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

// Gate is the automatic start of the generation. What it does is derived from what is stored; the one thing it keeps in memory is the causes of the
// refusals for a model that cannot be used that the user was told (nomodel.go). Until a Generator is set (SetGenerator) it is off: the Orchestrator stops after the Scene / Shot plan and the user starts each Shot by hand.
type Gate struct {
	d       Deps
	gen     Generator
	builder genplan.Builder

	mu      sync.Mutex
	noModel map[string]map[string]bool // Project ID -> the causes of the refusals for a model that cannot be used, as told
}

// New returns a Gate that is off.
func New(d Deps) *Gate {
	if d.Logger == nil {
		d.Logger = slog.New(slog.DiscardHandler)
	}
	return &Gate{d: d, noModel: map[string]map[string]bool{}}
}

// SetGenerator turns the gate on. config is the Model Adapter's current setting (the prices). Call it while the application is wired, before the job queue starts.
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
