// Package modelmanager is the model manager of the settings screen and of the MCP tools: the bundled ComfyUI
// engine (download, start, stop), the catalog of local image and video models (install, delete, licenses) and the
// read-only view of an external ComfyUI (spec/architecture/model-manager/model-manager.md, "Service API"). The trial
// generation outside any Project is a batch of the generation test service (package gentest).
//
// The engine and the models are below one directory next to mam.exe, so that the uninstaller removes them with the
// application. An installed model appears as a model of the managed endpoint "bundled-comfyui" (localmodels) and
// is then selectable everywhere a #831 model is. Downloads are background jobs (domain.JobModelInstall, resumable):
// Register installs the handler, JobStatus reads the progress.
//
// Like localmodels it does not import package service: its refusals are the Err values below and *InvalidError.
package modelmanager

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"sync"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localbackends"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/detect"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/engine"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/extract"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/gpu"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/track"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
)

// BundledEndpoint is the endpoint (kind comfyui) of the bundled engine: the engine's address, and the models that
// are installed. Its models are "comfyui:bundled-comfyui/<name>".
const BundledEndpoint = "bundled-comfyui"

// HuggingFaceProvider is the credential.Store provider of the optional Hugging Face token.
const HuggingFaceProvider = "huggingface"

// Errors of the service. A refusal that the user can fix is an *InvalidError (the message is shown).
var (
	// ErrUnsupported: the bundled engine exists on Windows only; elsewhere use an external ComfyUI.
	ErrUnsupported = errors.New("modelmanager: the bundled engine is available on Windows only")
	// ErrNotFound: no such model, job or trial.
	ErrNotFound = errors.New("modelmanager: not found")
	// ErrEngineNotInstalled: the engine is not installed (a model needs it).
	ErrEngineNotInstalled = errors.New("modelmanager: the engine is not installed")
	// ErrAlreadyInstalled: the model is installed.
	ErrAlreadyInstalled = errors.New("modelmanager: already installed")
	// ErrNotInstalled: the model is not installed.
	ErrNotInstalled = errors.New("modelmanager: not installed")
	// ErrConsentRequired: the license of the model has to be accepted first.
	ErrConsentRequired = errors.New("modelmanager: the license has to be accepted before the install")
	// ErrBusy: an install of the model is running.
	ErrBusy = errors.New("modelmanager: an install is running")
	// ErrNoGPU: no supported GPU was found and the CPU was not chosen; use an external ComfyUI or choose the CPU.
	ErrNoGPU = errors.New("modelmanager: no supported GPU was found")
	// ErrPinned: the operator pinned the model setting file (--model-config): the managed endpoint cannot be written.
	ErrPinned = modelsettings.ErrPinned
	// ErrEndpointMoved: the engine started on another port than the endpoint knew; the setting was updated, and
	// the call has to be made again.
	ErrEndpointMoved = errors.New("ComfyUI のポートが変わったため設定を更新しました。もう一度実行してください")
)

// InvalidError is a refused input; Message is shown to the user.
type InvalidError = modelsettings.InvalidError

func invalid(format string, args ...any) error {
	return &InvalidError{Message: fmt.Sprintf(format, args...)}
}

// Deps are what the Service needs.
type Deps struct {
	// Layout is the comfyui directory next to mam.exe (store.ForExecutable).
	Layout store.Layout
	// Catalog is the catalog (nil: catalog.Default()).
	Catalog *catalog.Catalog
	// Jobs and JobTable are the queue the install jobs run on and its table (read for their state).
	Jobs     *jobqueue.Queue
	JobTable *repository.JobRepository
	// Models creates and deletes the models of the bundled endpoint and lists the endpoints.
	Models *localmodels.Service
	// Adapter is the running Model Adapter: the setting the external ComfyUI view reads.
	Adapter *modeladapter.Adapter
	// Keys holds the optional Hugging Face token (HuggingFaceProvider).
	Keys credential.Store
	// Backends are the options the external ComfyUI view reads (staging directory).
	Backends localbackends.Options
	Logger   *slog.Logger

	// The following default to the real implementations; tests replace them.

	// Platform is the operating system for the bundled engine ("windows" supports it; default runtime.GOOS).
	Platform string
	// Extract unpacks the engine archive (default extract.SevenZip).
	Extract extract.Extractor
	// Fetch are the options of the downloads (client, URL check); Progress is set per download.
	Fetch fetch.Options
	// FreeBytes reports the free disk space at a path (default fsutil.FreeBytes).
	FreeBytes func(path string) (uint64, error)
	// Command builds the launch of the engine for a port (default: the installed engine).
	Command func(port int) (proc.Spec, error)
	// StartTimeout and PollEvery tune the start of the engine (defaults of proc.Config).
	StartTimeout time.Duration
	PollEvery    time.Duration
	// GPU lists the adapters and recommends a build (default gpu.DetectSystem).
	GPU func(ctx context.Context) gpu.Report
	// Comfy looks for a ComfyUI that is already there (default detect.Detect over the user's config directory).
	Comfy func(ctx context.Context) detect.Report
	Now   func() time.Time
}

// Service is the model manager.
type Service struct {
	Deps
	cat     *catalog.Catalog
	state   *store.Store
	engine  *engine.Installer
	proc    *proc.Manager
	tracker *track.Tracker
	bundled bool

	mu      sync.Mutex
	active  map[string]string // target -> job id of the queued or running job
	targets map[string]*sync.Mutex
	cancels map[string]context.CancelCauseFunc // job id -> cancel of a running install
}

// New returns the Service. It reads the record (a broken one is an error).
func New(d Deps) (*Service, error) {
	if d.Logger == nil {
		d.Logger = slog.New(slog.DiscardHandler)
	}
	if d.Catalog == nil {
		d.Catalog = catalog.Default()
	}
	if d.Platform == "" {
		d.Platform = runtime.GOOS
	}
	if d.Extract == nil {
		d.Extract = extract.SevenZip{}
	}
	if d.GPU == nil {
		d.GPU = gpu.DetectSystem
	}
	if d.Comfy == nil {
		d.Comfy = systemDetect
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	st, err := store.Open(d.Layout)
	if err != nil {
		return nil, err
	}
	s := &Service{Deps: d, cat: d.Catalog, state: st, bundled: d.Platform == "windows", tracker: track.New(),
		active: map[string]string{}, targets: map[string]*sync.Mutex{}, cancels: map[string]context.CancelCauseFunc{}}
	s.engine = &engine.Installer{Layout: d.Layout, Engine: d.Catalog.Engine, Store: st, Extract: d.Extract, Fetch: d.Fetch, FreeBytes: d.FreeBytes, Now: d.Now}
	build := d.Command
	if build == nil {
		build = s.engine.Command
	}
	s.proc = proc.New(proc.Config{
		Build: build, PreferredPort: func() int { return s.state.Get().LastPort },
		StartTimeout: d.StartTimeout, PollEvery: d.PollEvery, Logger: d.Logger,
	})
	return s, nil
}

// lock serializes the jobs of one target (two installs of the same model never write the same file).
func (s *Service) lock(target string) func() {
	s.mu.Lock()
	m, ok := s.targets[target]
	if !ok {
		m = &sync.Mutex{}
		s.targets[target] = m
	}
	s.mu.Unlock()
	m.Lock()
	return m.Unlock
}
