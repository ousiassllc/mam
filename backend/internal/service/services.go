// Package service is the application service layer. It implements use cases on
// top of agent and repository and is the only thing web/handler talks to.
package service

import (
	"context"
	"log/slog"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing"
	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/agent/continuation"
	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/agent/judge"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/orchestrator"
	"github.com/ousiassllc/mam/backend/internal/agent/repair"
	"github.com/ousiassllc/mam/backend/internal/agent/stateextractor"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/facesim"
	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// Deps are the infrastructure dependencies wired once in cmd/server.
type Deps struct {
	DB          *repository.DB
	Jobs        *jobqueue.Queue
	Credentials credential.Store
	Logger      *slog.Logger
	// AssetsDir is where uploaded files are stored (config.AssetsDir). Uploads fail when empty.
	AssetsDir string
	// Briefing extracts the Creative Brief from chat messages. Nil selects briefing.RuleExtractor.
	Briefing briefing.Extractor
	// LLM is the Reasoning LLM Adapter of the AUTO pipeline. When nil, or when Jobs is nil,
	// the pipeline is off: Projects stay in DRAFT after the Brief is complete.
	LLM llm.Completer
	// Models is the Model Adapter that generates images and videos. Generation (Shot Takes and
	// Reference Packs) is available only when it, LLM and Jobs are all set.
	Models *modeladapter.Adapter
	// FreeBytes reports the free disk space at a path. Nil selects fsutil.FreeBytes.
	FreeBytes func(path string) (uint64, error)
	// Frames samples frames from Takes (ffmpeg). The AI Video Judge is available only when it, LLM
	// and Jobs are all set.
	Frames judge.FrameSource
	// Faces is the local face engine of the Face Identity Similarity. Nil: Takes are judged
	// qualitatively only.
	Faces facesim.Engine
	// Media is ffmpeg / ffprobe for the Take previews (4K proxies, thumbnails) and Partial Take Use.
	// Nil: 4K Takes play as they are and Partial Take Use is unavailable.
	Media *media.Tools
	// Judge is the AI Video Judge setting. The zero value selects judge.DefaultConfig.
	Judge judge.Config
	// Jev is the TypeSafe Jev client of the Repair Agent's shadow run. Nil: no shadow run.
	Jev repair.Shadow
}

// Services is the DI root handed to router.New. Each feature adds one field
// and one line in New; nothing else in this file needs to change.
type Services struct {
	Health      *HealthService
	Credentials *CredentialService
	Projects    *ProjectService
	Chat        *ChatService
	Plans       *PlanService
	Generation  *GenerationService
	World       *WorldService
	Director    *DirectorService
	Policy      *PolicyService
	Judge       *JudgeService
	Repair      *RepairService
	Continue    *ContinueService
	Reference   *ReferenceService
	Trace       *TraceService
	Versions    *VersionService
	Bundle      *BundleService
	Series      *SeriesService
	Takes       *TakeService
	Notes       *NoteService
}

// Recover puts Shots left GENERATING by an abnormal termination back to PLANNING. It MUST run
// after New and before the job queue starts (their generate jobs are then failed by the queue).
func (s *Services) Recover(ctx context.Context) (int, error) {
	if s.Generation.gen == nil {
		return 0, nil
	}
	return s.Generation.gen.Recover(ctx)
}

// New builds every service from deps. When deps.Jobs is set, job status changes refresh
// the progress cards in the chat; New therefore MUST run before deps.Jobs.Start.
func New(deps Deps) *Services {
	logger := deps.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	extractor := deps.Briefing
	if extractor == nil {
		extractor = briefing.RuleExtractor{}
	}
	c := &core{
		projects:  repository.NewProjectRepository(deps.DB),
		chat:      repository.NewChatRepository(deps.DB),
		assets:    repository.NewAssetRepository(deps.DB),
		jobs:      repository.NewJobRepository(deps.DB),
		hub:       NewHub(),
		extractor: extractor,
		assetsDir: deps.AssetsDir,
		logger:    logger,
		plans:     repository.NewPlanRepository(deps.DB),

		references: repository.NewReferenceRepository(deps.DB),
	}
	chat := &ChatService{c}
	var jobFinished func(jobID string) // set once the Repair Agent exists (before the queue starts)
	if deps.Jobs != nil {
		deps.Jobs.OnChange(func(jobID string) {
			chat.JobChanged(context.Background(), jobID)
			if jobFinished != nil {
				jobFinished(jobID)
			}
		})
		if deps.LLM != nil {
			c.pipeline = orchestrator.New(orchestrator.Deps{
				Projects: c.projects, Plans: c.plans, ChatRepo: c.chat, Chat: chat, Jobs: deps.Jobs,
				LLM: deps.LLM, Notes: repository.NewNoteRepository(deps.DB), Logger: logger,
			})
			deps.Jobs.Register(domain.JobPlan, c.pipeline.Handle, true)
		}
	}
	gens := repository.NewGenerationRepository(deps.DB)
	notesRepo := repository.NewNoteRepository(deps.DB)
	world := repository.NewWorldRepository(deps.DB)
	policies := repository.NewPolicyRepository(deps.DB)
	generations := &GenerationService{core: c, shots: gens, progress: newBroker[generation.Progress]()}
	judges := &JudgeService{core: c, judges: repository.NewJudgeRepository(deps.DB), generations: generations}
	repairs := &RepairService{core: c, repairs: repository.NewRepairRepository(deps.DB), judges: judges.judges, generations: generations}
	takes := repository.NewTakeRepository(deps.DB)
	takeService := &TakeService{core: c, takes: takes, shots: gens, generations: generations, chatSvc: chat, tools: deps.Media}
	var takeJudge generation.Judger // stays nil (not a typed nil) while the Judge is off
	if deps.Jobs != nil && deps.LLM != nil && deps.Frames != nil {
		cfg := deps.Judge
		if cfg == (judge.Config{}) {
			cfg = judge.DefaultConfig()
		}
		judges.runner = judge.NewRunner(judge.Deps{
			Generations: gens, Judges: judges.judges, Assets: c.assets, Policies: policies, Chat: chat, Jobs: deps.Jobs,
			Takes: takes, Projects: c.projects,
			Judge:     judge.New(deps.LLM, deps.Frames, deps.Faces, cfg, logger),
			AssetsDir: deps.AssetsDir, Logger: logger,
			Notify: func(p generation.Progress) { generations.progress.Publish(p.ShotID, p) },
		})
		// Resumable: judging is repeatable and cheap, so an interrupted run is run again.
		deps.Jobs.Register(domain.JobJudge, judges.runner.Handle, true)
		takeJudge = judges.runner
	}
	if deps.Jobs != nil && deps.LLM != nil && deps.Models != nil {
		generations.gen = generation.New(generation.Deps{
			Generations: gens, World: world, Notes: notesRepo, Projects: c.projects, Policies: policies, FreeBytes: deps.FreeBytes, Assets: c.assets, Chat: chat, Jobs: deps.Jobs,
			LLM: deps.LLM, Adapter: deps.Models, AssetsDir: deps.AssetsDir, Logger: logger, Judge: takeJudge,
			Notify: func(p generation.Progress) { generations.progress.Publish(p.ShotID, p) },
		})
		// Not resumable: a generation costs money, so an interrupted one is failed, not repeated.
		deps.Jobs.Register(domain.JobGenerate, generations.gen.Handle, false)
		if judges.runner != nil {
			repairDeps := repair.Deps{
				Generations: gens, Judges: judges.judges, Repairs: repairs.repairs, JobRepo: c.jobs, Policies: policies, Projects: c.projects,
				Regenerator: generations.gen, Chat: chat, Jobs: deps.Jobs, LLM: deps.LLM, Shadow: deps.Jev, Logger: logger,
				Notify: func(p generation.Progress) { generations.progress.Publish(p.ShotID, p) },
			}
			if deps.Media != nil { // splicing a Partial Regeneration into its Take needs ffmpeg
				repairDeps.Splicer, repairDeps.Judge = takeService, judges.runner
			}
			repairs.agent = repair.New(repairDeps)
			// Not resumable: a repair costs money, so an interrupted one is failed, not repeated.
			deps.Jobs.Register(domain.JobRepair, repairs.agent.Handle, false)
			// The Repair / Retake Loop continues when a generate or judge job has finished (the queue's
			// callback must not block).
			jobFinished = func(jobID string) { go repairs.agent.JobChanged(context.Background(), jobID) }
		}
	}
	versions := repository.NewVersionRepository(deps.DB)
	director := &DirectorService{core: c, gens: gens, locks: repository.NewLockRepository(deps.DB), versions: versions, orch: generations.gen, takes: takes, repair: repairs.agent}
	if deps.LLM != nil {
		director.interpreter = commandinterpreter.New(deps.LLM)
	}
	c.director = director
	continues := &ContinueService{core: c, generation: generations, shots: gens}
	if deps.LLM != nil {
		continues.agent = continuation.New(deps.LLM)
	}
	c.continuer = continues
	notes := &NoteService{core: c, notes: notesRepo, series: repository.NewSeriesRepository(deps.DB), world: world}
	if deps.LLM != nil {
		notes.extractor = stateextractor.New(deps.LLM)
	}
	c.notes = notes
	worlds := &WorldService{core: c, world: world, versions: versions}
	return &Services{
		Health:      NewHealthService(deps.DB),
		Credentials: NewCredentialService(deps.Credentials),
		Projects:    &ProjectService{c},
		Chat:        chat,
		Plans:       &PlanService{c},
		Generation:  generations,
		World:       worlds,
		Director:    director,
		Policy: &PolicyService{
			core: c, freeBytes: deps.FreeBytes, policies: policies, costs: repository.NewCostRepository(deps.DB), storage: repository.NewStorageRepository(deps.DB),
		},
		Judge:     judges,
		Repair:    repairs,
		Continue:  continues,
		Reference: &ReferenceService{c},
		Trace:     &TraceService{traces: repository.NewTraceRepository(deps.DB)},
		Versions:  &VersionService{core: c, versions: versions, director: director, world: worlds},
		Bundle:    &BundleService{core: c, bundles: repository.NewBundleRepository(deps.DB)},
		Series:    &SeriesService{core: c, series: repository.NewSeriesRepository(deps.DB)},
		Takes:     takeService,
		Notes:     notes,
	}
}
