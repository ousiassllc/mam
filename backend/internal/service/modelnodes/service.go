// Package modelnodes is the custom nodes of the model manager (spec/architecture/model-manager/custom-nodes.md): the allowlist of the nodes
// the bundled ComfyUI may load (modelmgr/nodecat), each with its license, commercial use, dependencies and review, and their install and
// removal as model_install jobs with the target "node:<id>". An install fetches the pinned archive, checks its SHA-256, extracts it below
// custom_nodes, restarts the engine when it was running, and checks that ComfyUI loaded the node (/object_info); the engine is started
// with --whitelist-custom-nodes naming only the folders that are installed and unchanged (modelmgr/nodes).
//
// It is the counterpart of package localruntime: like it, it shares the job kind, the downloader and the engine process with the model
// manager and keeps its own jobs. Like modelmanager it does not import package service: its refusals are *InvalidError and the Err values
// of modelmanager, which the screens and the MCP tools already map.
package modelnodes

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/nodes"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/track"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

// TargetPrefix starts the target of a job of a node ("node:ipadapter-plus").
const TargetPrefix = "node:"

// Errors of the service (the refusals that need no message of their own are modelmanager's: ErrNotFound, ErrBusy, ErrUnsupported,
// ErrEngineNotInstalled, ErrNotInstalled).
var (
	// errCancelled is why an install the user stopped failed. The download stays for a resume.
	errCancelled = errors.New("導入を中断しました。もう一度導入すると、ダウンロード済みの続きから再開します")
)

// InvalidError is a refused input; Message is shown to the user.
type InvalidError = modelmanager.InvalidError

func invalid(format string, args ...any) error {
	return &InvalidError{Message: fmt.Sprintf(format, args...)}
}

// Engine is the part of the model manager the service uses: whether the bundled engine can take a node, and the process it restarts.
type Engine interface {
	Engine() modelmanager.EngineView
	Catalog() []modelmanager.ModelView
	StopEngine() error
	StartEngine(ctx context.Context) (proc.Status, error)
}

// Loaded asks a running engine whether it loaded the classes of a node: the classes of the node that /object_info does not list. The default
// reads GET <url>/object_info/<class> over loopback; tests replace it.
type Loaded func(ctx context.Context, baseURL string, classes []string) (missing []string, err error)

// Deps are what the Service needs.
type Deps struct {
	// Nodes is the manager the engine's whitelist is read from: the same one the model manager was given (modelmanager.Deps.Nodes).
	Nodes *nodes.Manager
	// Engine is the model manager (it owns the engine process).
	Engine Engine
	// Queue and JobTable are the queue the install jobs run on and its table (read for their state).
	Queue    *jobqueue.Queue
	JobTable *repository.JobRepository
	Logger   *slog.Logger
	// Loaded checks a started engine for the classes of a node (nil: the HTTP check).
	Loaded Loaded
	Now    func() time.Time
}

// Service is the custom node manager.
type Service struct {
	Deps
	tracker *track.Tracker
	mu      sync.Mutex
	active  map[string]string // target -> job id of the queued or running job
	cancels map[string]context.CancelCauseFunc
}

// New returns the Service.
func New(d Deps) *Service {
	if d.Logger == nil {
		d.Logger = slog.New(slog.DiscardHandler)
	}
	if d.Loaded == nil {
		d.Loaded = loadedOverHTTP
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	return &Service{Deps: d, tracker: track.New(), active: map[string]string{}, cancels: map[string]context.CancelCauseFunc{}}
}

// OnStartup removes what an interrupted install left in custom_nodes.
func (s *Service) OnStartup() { s.Nodes.CleanLeftovers() }

func (s *Service) activeJob(target string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active[target]
}

func (s *Service) release(target, jobID string) {
	s.mu.Lock()
	if s.active[target] == jobID {
		delete(s.active, target)
	}
	s.mu.Unlock()
}

// cancellable returns ctx with a cancel that CancelInstall reaches by the id of the job, and what to call when the handler ends.
func (s *Service) cancellable(ctx context.Context, jobID string) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(ctx)
	s.mu.Lock()
	s.cancels[jobID] = cancel
	s.mu.Unlock()
	return ctx, func() {
		s.mu.Lock()
		delete(s.cancels, jobID)
		s.mu.Unlock()
		cancel(nil)
	}
}

// CancelInstall stops a running install by the id of its job. The job ends as failed with a message that says so; installing again resumes
// the download. A job that is not running (queued, ended, or not a node's) is modelmanager.ErrNotFound.
func (s *Service) CancelInstall(jobID string) error {
	s.mu.Lock()
	cancel, ok := s.cancels[jobID]
	s.mu.Unlock()
	if !ok {
		return modelmanager.ErrNotFound
	}
	cancel(errCancelled)
	return nil
}
