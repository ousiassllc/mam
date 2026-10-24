// Package service is the application service layer. It implements use cases on
// top of agent and repository and is the only thing web/handler talks to.
package service

import (
	"context"
	"log/slog"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/agent/briefing"
	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/agent/judge"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/repair"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/facesim"
	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
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
	// LLM is the Reasoning LLM Adapter of the AUTO pipeline; nil (or nil Jobs) turns it off: Projects stay in DRAFT.
	LLM llm.Completer
	// Models is the Model Adapter that generates images and videos; Takes and Reference Packs need it, LLM and Jobs.
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
	// Audio are the vendors of the Audio Director: ElevenLabs (TTS, cloned voices, Music, Sound
	// Effects) and Sync Labs (Lip Sync). Audio production is available only when Audio.Speech and
	// Media are set; a missing Music / Sounds / LipSync leaves that layer out.
	Audio audiodirector.Providers
}

// Services is the DI root handed to router.New: one field per feature and one line in wiring.services (wiring.go).
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
	Branch      *BranchService
	Review      *ReviewService
	Editor      *EditorService
	Brand       *BrandService
	Analytics   *AnalyticsService
	LocalModels *localmodels.Service // ComfyUI / OpenAI-compatible endpoints and models; the app sets it
	// ModelManager: bundled ComfyUI engine, model catalog, trial generation; the app sets it.
	ModelManager *modelmanager.Service
}

// Close stops the background workers of the services and kills the Take preview conversions that
// are running. Call it on shutdown, after the job queue stopped.
func (s *Services) Close() { s.Takes.previews.Close(); s.Editor.Close() }

// Recover puts Shots left GENERATING, REPAIRING or JUDGING by an abnormal termination back to PLANNING.
// It MUST run after New and before the job queue starts, twice: before the queue resolves its running
// jobs (their generate jobs are then failed by it) and after (a judge job it failed leaves its Shot
// JUDGING with no handler to release it). A Shot with a job that will still run keeps its state.
func (s *Services) Recover(ctx context.Context) (int, error) {
	return s.Generation.shots.ResetStaleShots(ctx)
}

// New builds every service from deps. When deps.Jobs is set, job status changes refresh
// the progress cards in the chat; New therefore MUST run before deps.Jobs.Start.
//
// The order is the dependency order: an agent is wired after the agents it holds, and the queue's
// OnChange callback last (see wiring).
func New(deps Deps) *Services {
	w := newWiring(deps)
	w.wirePlan()
	w.wireJudge()
	w.wireGeneration()
	w.wireRepair()
	w.wireDirector()
	w.wireContinuity()
	w.wireEditor()
	w.watchJobs()
	return w.services()
}
