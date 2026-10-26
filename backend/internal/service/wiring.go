package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/agent/briefing"
	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/agent/continuation"
	"github.com/ousiassllc/mam/backend/internal/agent/editor"
	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/agent/generation/relay"
	"github.com/ousiassllc/mam/backend/internal/agent/judge"
	"github.com/ousiassllc/mam/backend/internal/agent/orchestrator"
	"github.com/ousiassllc/mam/backend/internal/agent/repair"
	"github.com/ousiassllc/mam/backend/internal/agent/stateextractor"
	"github.com/ousiassllc/mam/backend/internal/bgwork"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/previewcache"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/bundle"
	"github.com/ousiassllc/mam/backend/internal/repository/review"
)

// repos are the repositories more than one service uses. Each is built once, so that a repository
// that comes to hold state (a cache, a lock) is still one instance for everybody.
type repos struct {
	gens     *repository.GenerationRepository
	notes    *repository.NoteRepository
	world    *repository.WorldRepository
	policies *repository.PolicyRepository
	brands   *repository.BrandRepository
	judges   *repository.JudgeRepository
	repairs  *repository.RepairRepository
	takes    *repository.TakeRepository
	versions *repository.VersionRepository
	locks    *repository.LockRepository
	series   *repository.SeriesRepository
	costs    *repository.CostRepository
}

func newRepos(db *repository.DB) repos {
	return repos{
		gens:     repository.NewGenerationRepository(db),
		notes:    repository.NewNoteRepository(db),
		world:    repository.NewWorldRepository(db),
		policies: repository.NewPolicyRepository(db),
		brands:   repository.NewBrandRepository(db),
		judges:   repository.NewJudgeRepository(db),
		repairs:  repository.NewRepairRepository(db),
		takes:    repository.NewTakeRepository(db),
		versions: repository.NewVersionRepository(db),
		locks:    repository.NewLockRepository(db),
		series:   repository.NewSeriesRepository(db),
		costs:    repository.NewCostRepository(db),
	}
}

// wiring holds what New has built so far. The wire* methods run in the order New calls them: each
// reads only what the ones before it set, and the job queue's OnChange callback is installed last
// (watchJobs), when every agent it calls exists.
type wiring struct {
	deps   Deps
	logger *slog.Logger
	r      repos
	c      *core
	chat   *ChatService

	generations *GenerationService
	judges      *JudgeService
	repairs     *RepairService
	takes       *TakeService
	worlds      *WorldService
	director    *DirectorService
	continues   *ContinueService
	notes       *NoteService
	branch      *BranchService
	editors     *EditorService

	takeJudge generation.Judger // stays nil (not a typed nil) while the Judge is off
}

// newWiring builds the services that exist whatever deps are set; the wire* methods then add the
// agents the deps allow.
func newWiring(deps Deps) *wiring {
	logger := deps.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	extractor := deps.Briefing
	if extractor == nil {
		extractor = briefing.RuleExtractor{}
	}
	r := newRepos(deps.DB)
	c := &core{
		projects:  repository.NewProjectRepository(deps.DB),
		chat:      repository.NewChatRepository(deps.DB),
		assets:    repository.NewAssetRepository(deps.DB),
		jobs:      repository.NewJobRepository(deps.DB),
		hub:       NewHub(),
		extractor: extractor,
		assetsDir: deps.AssetsDir,
		previews:  previewcache.New(logger, deps.AssetsDir, 4<<30), // the oldest previews beyond 4 GiB are deleted when a new one is made
		logger:    logger,
		plans:     repository.NewPlanRepository(deps.DB),

		references: repository.NewReferenceRepository(deps.DB),
		branches:   repository.NewBranchRepository(deps.DB),

		brandTemplates: repository.NewBrandTemplateRepository(deps.DB),
		analytics:      repository.NewAnalyticsRepository(deps.DB),
	}
	c.takeActions = takeActions{judges: r.judges, policies: r.policies}
	chat := &ChatService{c}
	generations := &GenerationService{core: c, shots: r.gens, progress: newBroker[generation.Progress]()}
	judges := &JudgeService{core: c, judges: r.judges, generations: generations}
	return &wiring{
		deps: deps, logger: logger, r: r, c: c, chat: chat,
		generations: generations,
		judges:      judges,
		repairs:     &RepairService{core: c, repairs: r.repairs, judges: r.judges, generations: generations},
		takes:       &TakeService{core: c, takes: r.takes, shots: r.gens, generations: generations, chatSvc: chat, tools: deps.Media},
		worlds:      &WorldService{core: c, world: r.world, versions: r.versions},
	}
}

// agentsOn reports whether the Reasoning LLM agents can run: they need the queue and the LLM.
func (w *wiring) agentsOn() bool { return w.deps.Jobs != nil && w.deps.LLM != nil }

// notifyProgress publishes the progress of a Shot to the clients that watch it.
func (w *wiring) notifyProgress(p generation.Progress) { w.generations.progress.Publish(p.ShotID, p) }

// wirePlan sets up the AUTO pipeline (job "plan"): needs the queue and the LLM.
func (w *wiring) wirePlan() {
	if !w.agentsOn() {
		return
	}
	d := w.deps
	w.c.pipeline = orchestrator.New(orchestrator.Deps{
		Projects: w.c.projects, Plans: w.c.plans, ChatRepo: w.c.chat, Chat: w.chat, Jobs: d.Jobs,
		LLM: d.LLM, Notes: w.r.notes, Logger: w.logger,
	})
	d.Jobs.Register(domain.JobPlan, w.c.pipeline.Handle, true)
}

// wireJudge sets up the AI Video Judge (job "judge"): needs the queue, the LLM and Frames.
func (w *wiring) wireJudge() {
	d := w.deps
	if !w.agentsOn() || d.Frames == nil {
		return
	}
	cfg := d.Judge
	if cfg == (judge.Config{}) {
		cfg = judge.DefaultConfig()
	}
	w.judges.runner = judge.NewRunner(judge.Deps{
		Generations: w.r.gens, Judges: w.r.judges, Assets: w.c.assets, Policies: w.r.policies, Chat: w.chat, Jobs: d.Jobs,
		Takes: w.r.takes, Projects: w.c.projects, Brands: w.r.brands,
		Judge:     judge.New(d.LLM, d.Frames, d.Faces, cfg, w.logger),
		AssetsDir: d.AssetsDir, Logger: w.logger,
		Notify:     w.notifyProgress,
		TakeJudged: w.chat.TakeJudged,
	})
	// Resumable: judging is repeatable and cheap, so an interrupted run is run again.
	d.Jobs.Register(domain.JobJudge, w.judges.runner.Handle, true)
	w.c.takeActions.judgeEnabled = true
	w.takeJudge = w.judges.runner
}

// wireGeneration sets up the generation of Shot Takes (job "generate"): needs the queue, the LLM
// and the Model Adapter. It runs after wireJudge, whose Judge it hands the Takes to.
func (w *wiring) wireGeneration() {
	d := w.deps
	if !w.agentsOn() || d.Models == nil {
		return
	}
	progress := relay.New(w.notifyProgress)
	w.generations.gen = generation.New(generation.Deps{
		Generations: w.r.gens, World: w.r.world, Notes: w.r.notes, Projects: w.c.projects, Policies: w.r.policies, FreeBytes: d.FreeBytes, Assets: w.c.assets, Chat: w.chat, Jobs: d.Jobs,
		Repairs: w.r.repairs, LLM: d.LLM, Adapter: d.Models, AssetsDir: d.AssetsDir, Logger: w.logger, Judge: w.takeJudge, Brands: w.r.brands, Media: d.Media,
		Notify: progress.Notify,
	})
	// Not resumable: a generation costs money, so an interrupted one is failed, not repeated.
	d.Jobs.Register(domain.JobGenerate, progress.Wrap(w.generations.gen.Handle), false)
	w.c.takeActions.gen = w.generations.gen
}

// wireRepair sets up the Repair Agent (job "repair"): needs generation and the Judge, so everything
// they need. It runs after both.
func (w *wiring) wireRepair() {
	d := w.deps
	if w.generations.gen == nil || w.judges.runner == nil {
		return
	}
	repairDeps := repair.Deps{
		Generations: w.r.gens, Judges: w.r.judges, Repairs: w.r.repairs, JobRepo: w.c.jobs, Policies: w.r.policies, Projects: w.c.projects, Takes: w.r.takes,
		Regenerator: w.generations.gen, Chat: w.chat, Jobs: d.Jobs, LLM: d.LLM, Shadow: d.Jev, Logger: w.logger,
		Notify: w.notifyProgress,
	}
	if d.Media != nil { // splicing a Partial Regeneration into its Take needs ffmpeg
		repairDeps.Splicer, repairDeps.Judge = w.takes, w.judges.runner
	}
	w.repairs.agent = repair.New(repairDeps)
	// Not resumable: a repair costs money, so an interrupted one is failed, not repeated.
	d.Jobs.Register(domain.JobRepair, w.repairs.agent.Handle, false)
}

// wireDirector sets up DIRECTOR MODE. It runs after wireGeneration and wireRepair, whose agents it holds.
func (w *wiring) wireDirector() {
	w.director = &DirectorService{
		core: w.c, gens: w.r.gens, locks: w.r.locks, versions: w.r.versions, orch: w.generations.gen,
		takes: w.r.takes, repair: w.repairs.agent, judges: w.r.judges, brands: w.r.brands,
	}
	if w.deps.LLM != nil {
		w.director.interpreter = commandinterpreter.New(w.deps.LLM)
	}
	w.c.director = w.director
}

// wireContinuity sets up the services that carry a story on: Continue, Notes and Branch.
func (w *wiring) wireContinuity() {
	w.continues = &ContinueService{core: w.c, generation: w.generations, shots: w.r.gens, brands: w.r.brands, tools: w.deps.Media}
	if w.deps.LLM != nil {
		w.continues.agent = continuation.New(w.deps.LLM)
	}
	w.c.continuer = w.continues
	w.notes = &NoteService{core: w.c, notes: w.r.notes, series: w.r.series, world: w.r.world}
	if w.deps.LLM != nil {
		w.notes.extractor = stateextractor.New(w.deps.LLM, stateextractor.WithLogger(w.logger))
	}
	w.c.notes = w.notes
	w.branch = &BranchService{core: w.c, shots: w.r.gens, continues: w.continues, brands: w.r.brands}
	w.director.branch = w.branch
}

// wireEditor sets up the Editor and, with ffmpeg and a speech vendor, the Audio Director.
func (w *wiring) wireEditor() {
	d := w.deps
	// Without a Reasoning LLM the Editor Judge runs its deterministic checks alone.
	w.editors = &EditorService{
		core: w.c, editors: repository.NewEditorRepository(d.DB), shots: w.r.gens, generation: w.generations, policies: w.r.policies, takes: w.r.takes, locks: w.r.locks, chatSvc: w.chat, tools: d.Media,
		judge: editor.NewJudge(d.LLM), brands: w.r.brands, judges: w.r.judges,
		voices: repository.NewVoiceRepository(d.DB), cloner: d.Audio.Cloner,
	}
	w.editors.resume = bgwork.NewWorker(w.logger, "editor resume", w.editors.resumeAll)
	w.takes.afterSelect = w.editors.ResumeEditing
	// An Approval card answered in the editing half (Rough Cut, Final) lets the AUTO chain go on.
	w.c.afterAnswer = w.editors.RetryAuto
	if d.Media != nil && d.Audio.Speech != nil {
		// Without a Reasoning LLM the Audio Director plans its layers by rule (tempo, Brief mood, location).
		w.editors.audio = audiodirector.New(audiodirector.Deps{
			Providers: d.Audio, Planner: audiodirector.NewPlanner(d.LLM), Ledger: w.r.costs,
			Tools: *d.Media, Root: d.AssetsDir,
		})
	}
}

// watchJobs refreshes the chat when a job changes status. It is the only OnChange of the queue and
// runs after every agent it calls was wired; the queue MUST NOT have started (OnChange panics then).
func (w *wiring) watchJobs() {
	if w.deps.Jobs == nil {
		return
	}
	var repairLoop func(jobID string)
	if agent := w.repairs.agent; agent != nil {
		// The Repair / Retake Loop continues when a generate or judge job has finished (the queue's
		// callback must not block, and a panic must not end the server).
		repairLoop = bgwork.Async(w.logger, "repair loop", func(jobID string) { agent.JobChanged(context.Background(), jobID) })
	}
	w.deps.Jobs.OnChange(func(jobID string) {
		w.chat.JobChanged(context.Background(), jobID)
		if repairLoop != nil {
			repairLoop(jobID)
		}
		// A finished generation / judge job may complete an additional Shot of the Editor → Reshoot Loop.
		w.editors.ResumeEditing()
	})
}

// services assembles the DI root from what was wired.
func (w *wiring) services() *Services {
	c, d := w.c, w.deps
	return &Services{
		Health:      NewHealthService(d.DB),
		Credentials: NewCredentialService(d.Credentials),
		Projects:    &ProjectService{c},
		Chat:        w.chat,
		Plans:       &PlanService{c},
		Generation:  w.generations,
		World:       w.worlds,
		Director:    w.director,
		Policy: &PolicyService{
			core: c, freeBytes: d.FreeBytes, policies: w.r.policies, costs: w.r.costs, storage: repository.NewStorageRepository(d.DB),
		},
		Judge:     w.judges,
		Repair:    w.repairs,
		Continue:  w.continues,
		Reference: &ReferenceService{c},
		Trace:     &TraceService{traces: repository.NewTraceRepository(d.DB)},
		Versions:  &VersionService{core: c, versions: w.r.versions, director: w.director, world: w.worlds},
		Bundle:    &BundleService{core: c, bundles: bundle.New(d.DB), freeBytes: d.FreeBytes},
		Series:    &SeriesService{core: c, series: w.r.series},
		Takes:     w.takes,
		Notes:     w.notes,
		Branch:    w.branch,
		Editor:    w.editors,
		Brand:     &BrandService{core: c, brands: w.r.brands},
		Review:    &ReviewService{core: c, reviews: review.New(d.DB), editors: w.editors.editors, chatSvc: w.chat, now: time.Now},
		Analytics: &AnalyticsService{c},
	}
}
