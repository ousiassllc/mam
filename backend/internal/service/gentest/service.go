// Package gentest is the generation test service: the screen /generation-test and its MCP tools generate images and videos
// outside any Project, compare models and prompts side by side, keep the history and import a result into a Project
// (spec/architecture/gentest/gentest.md). It is the one implementation behind the trial generation of the model manager too.
//
// A batch is every combination of the chosen models, prompts and a count per combination. It is estimated first
// (Estimate), run only with the estimate the caller confirmed (Run: a changed estimate, or one over the limits, is refused),
// and runs as a job of kind domain.JobGentest, which is not resumable: a generation is paid for, so after an abnormal
// termination the results that were not over are failed (OnStartup) and the results that succeeded stay. Every call
// is a direct call of the Model Adapter (package direct): the model named, the prompt as typed, no fall-back.
//
// Like modelmanager it does not import package service: its refusals are those of package gterr (*gterr.InvalidError, *gterr.ConflictError, gterr.ErrNotFound).
// Nothing here writes a Take, a Cost Ledger row or an asset of a Project (Import writes the asset only when asked to).
package gentest

import (
	"context"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/history"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/loras"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/poses"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/projectlink"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/runner"

	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyui/live"
	"github.com/ousiassllc/mam/backend/internal/agent/prompttranslate"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
	"github.com/ousiassllc/mam/backend/internal/repository/reciperepo"
)

// Limits of a request.
const (
	// MaxPromptRunes is the longest prompt (and negative prompt), in characters.
	MaxPromptRunes = 4000
	// MaxReferences is the most input images of a batch.
	MaxReferences = 10
	// MaxImageBytes is the largest input image.
	MaxImageBytes = 20 << 20
	// MaxCount is the most results per model and prompt (the batch limit of models.json applies as well).
	MaxCount = 20
)

// Deps are what the Service needs.
type Deps struct {
	Repo     *gentestrepo.Repository
	Jobs     *jobqueue.Queue
	JobTable *repository.JobRepository
	Adapter  *modeladapter.Adapter
	// DataDir holds the history's files below DataDir/gentest.
	DataDir string
	// AssetsDir is where an imported file is stored (config.AssetsDir).
	AssetsDir string
	// Projects, Assets and References are read to pick inputs from a Project and written to import a result.
	Projects   *repository.ProjectRepository
	Assets     *repository.AssetRepository
	References *repository.ReferenceRepository
	// Assign is the optional second step of an import (nil: an import with a slot is refused).
	Assign projectlink.AssignFunc
	// Explain says in words for the user why a call failed, when the failure is one of the app's own refusals (the bundled
	// engine is not installed, ...); "" leaves the failure as it is. May be nil.
	Explain func(err error) string
	// Hidden says a model is left out of the model lists and refused as unknown: the adult models of the model manager while
	// the age gate is closed (modelmanager.Service.HidesModel). May be nil.
	Hidden func(modelID string) bool
	// MissingNodes gives the reason a model of the bundled ComfyUI cannot be chosen because a custom node its workflow requires is not installed
	// (modelnodes, nodegate.ForModels); empty when it can. Nil: no model is refused for its nodes.
	MissingNodes func(modelID string, required []string) string
	// PlatformWarning says a model is not for this PC: the OS / GPU generation columns of the model catalog (Issue #979) put it outside, so that
	// the easy tier does not pick it for an intent ("" when it is fine). Nil: no model is ruled out.
	PlatformWarning func(ctx context.Context, modelID string) string
	// Recipes keeps the recipes of the image form (package recipe, Issue #977). Nil: none can be saved or used.
	Recipes *reciperepo.Repository
	// Poses is the pose library (Shisei), whose pictures can be given as references (FromPoseLibrary). Nil: no pose library; such a reference is refused.
	Poses *poses.Library
	// BundledEndpoint is the name of the registered endpoint of the bundled ComfyUI engine, whose LoRA files LoRAs lists ("": none).
	BundledEndpoint string
	// FaceCrop cuts the face out of a reference of the role "face" before an editing model reads it (nil: the picture goes as it is).
	FaceCrop runner.FaceCropFunc
	// Translator translates the prompts of a batch that asks for it (nil: such a batch is refused unless every prompt is English).
	Translator *prompttranslate.Translator
	Logger     *slog.Logger
	// Now and Seed are for tests.
	Now  func() time.Time
	Seed func() int64
}

// Service is the generation test service.
type Service struct {
	Deps
	// Link is the Project side: the inputs taken from a Project and the import of a result (its methods are the service's).
	*projectlink.Link
	// Runner runs the batches (Generate, Cancel; the prompt match sets its Extra, the runner of the batches that plan their rounds
	// themselves) and Store is the history (its methods are the service's).
	*runner.Runner
	*history.Store
	catalog modelcatalog.Catalog
	loras   loras.Lister
}

// New returns the Service.
func New(d Deps) *Service {
	if d.Logger == nil {
		d.Logger = slog.New(slog.DiscardHandler)
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Seed == nil {
		d.Seed = randomSeed
	}
	dir := filepath.Join(d.DataDir, DirName)
	link := projectlink.New(projectlink.Deps{
		Repo: d.Repo, Dir: dir, AssetsDir: d.AssetsDir, Projects: d.Projects, References: d.References,
		Assign: d.Assign, Now: d.Now, Logger: d.Logger,
	})
	run := runner.New(runner.Deps{
		Repo: d.Repo, Adapter: d.Adapter, Dir: dir, Explain: d.Explain, Hidden: d.Hidden, FaceCrop: d.FaceCrop, Now: d.Now, Logger: d.Logger,
	})
	return &Service{
		Deps: d, Link: link, Runner: run, Store: history.New(d.Repo, dir), catalog: modelcatalog.Catalog{Adapter: d.Adapter, Hidden: d.Hidden, MissingNodes: d.MissingNodes},
	}
}

// Register installs the handler of domain.JobGentest (not resumable). Call it before the job queue starts.
func (s *Service) Register() {
	s.Jobs.Register(domain.JobGentest, live.OwnedByJob(s.RunJob), false)
}

// OnStartup fails the results an abnormal termination left queued or running and removes staged inputs that no batch took.
// Call it after the job queue recovered its jobs and before it starts.
func (s *Service) OnStartup(ctx context.Context) {
	n, err := s.Repo.FailUnfinished(ctx, runner.ReasonInterrupted, s.Now().UTC())
	if err != nil {
		s.Logger.Error("settle unfinished generation test results failed", "err", err)
	} else if n > 0 {
		s.Logger.Warn("failed generation test results that were interrupted", "count", n)
	}
	s.cleanStaging()
}

func (s *Service) dir() string { return filepath.Join(s.DataDir, DirName) }

// DirName is the directory of the history's files below the data directory.
const DirName = "gentest"
