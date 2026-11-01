// Package localruntime is the manager of the local AI runtimes (GPT-SoVITS now, MuseTalk later): for each runtime of the catalog it
// installs the vendor's pinned package next to mam.exe (as a model_install job with the target "runtime:<id>"), starts and stops its
// server process, and says what is installed, whether this machine can run it and why not (spec/architecture/model-manager/
// local-runtime.md). It is the counterpart of the bundled ComfyUI engine of package modelmanager, with which it shares the job
// kind, the downloader, the extractor and the process manager, and none of its state.
//
// Like modelmanager it does not import package service: its refusals are the Err values below and *SupportError.
package localruntime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/extract"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/gpu"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	rtm "github.com/ousiassllc/mam/backend/internal/modelmgr/runtime"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/track"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// TargetPrefix starts the target of an install job of a runtime ("runtime:gpt-sovits").
const TargetPrefix = "runtime:"

// Errors of the service. A refusal because of the machine is a *SupportError (the message is shown).
var (
	// ErrNotFound: no such runtime or job.
	ErrNotFound = errors.New("localruntime: not found")
	// ErrUnsupported: the machine cannot run the runtime (a *SupportError carries the reason; errors.Is reaches this).
	ErrUnsupported = errors.New("localruntime: this machine cannot run the runtime")
	// ErrNotInstalled: the runtime is not installed (or its files are gone).
	ErrNotInstalled = errors.New("localruntime: the runtime is not installed")
	// ErrBusy: an install of the runtime is queued or running.
	ErrBusy = errors.New("localruntime: an install is running")
	// ErrWeightsMissing: the runtime is installed but a weight or dependency it needs is not there or not verified (a
	// *WeightsError names them; errors.Is reaches this). Start refuses before the server would fail in a way no one can read.
	ErrWeightsMissing = errors.New("localruntime: the weights of the runtime are missing")
	// ErrSourceGone: a weight answered 404 or 410 at its pinned URL, the repository or the file was deleted (a mirror that went away
	// is the case the catalog expects, AssetSource.Note). The message names the file and where the original is.
	ErrSourceGone = errors.New("localruntime: the source of a weight is gone")
)

// WeightsError is the refusal to start a runtime whose weights are not all there: Assets are the required ones that are not.
type WeightsError struct {
	Runtime string
	Name    string
	Assets  []rtm.AssetStatus
}

func (e *WeightsError) Error() string {
	names := make([]string, 0, len(e.Assets))
	missing := false
	for _, a := range e.Assets {
		names = append(names, a.Name)
		missing = missing || a.State == rtm.AssetMissing
	}
	what := "重みの確認が済んでいません"
	if missing {
		what = "重みが不足しています"
	}
	return fmt.Sprintf("%s の%s（%s）。「重みを導入」を実行してください", e.Name, what, strings.Join(names, "、"))
}

// Is makes errors.Is(err, ErrWeightsMissing) true.
func (e *WeightsError) Is(target error) bool { return target == ErrWeightsMissing }

// SupportError is the refusal of an install or a start on a machine that cannot run the runtime: Support has the code, the reason
// and what to use instead (the cloud provider). Its message is shown as it is.
type SupportError struct {
	Runtime string
	Support rtm.Support
}

func (e *SupportError) Error() string { return e.Support.Message() }

// Is makes errors.Is(err, ErrUnsupported) true.
func (e *SupportError) Is(target error) bool { return target == ErrUnsupported }

// Deps are what the Service needs.
type Deps struct {
	// Root is the localai directory next to mam.exe (store.LocalAIRoot); the runtimes are its subdirectories.
	Root string
	// Catalog is the catalog (nil: catalog.Default()).
	Catalog *catalog.Catalog
	// Queue is the job queue the install jobs run on. Register takes the handler of model_install over from the model manager.
	Queue Queue
	// JobTable is the table of the queue, read for the state of an install job (JobStatus).
	JobTable *repository.JobRepository
	// Logger logs the lifecycle of the processes (nil: discard).
	Logger *slog.Logger

	// The following default to the real implementations; tests replace them.

	// Platform is the OS and architecture of the machine (default: this one).
	Platform rtm.Platform
	// GPU lists the adapters (default gpu.DetectSystem).
	GPU func(ctx context.Context) gpu.Report
	// Extract unpacks the archive (default extract.SevenZip).
	Extract extract.Extractor
	// Fetch are the options of the downloads (client, URL check); Progress is set per download.
	Fetch fetch.Options
	// FreeBytes reports the free disk space at a path (default fsutil.FreeBytes).
	FreeBytes func(path string) (uint64, error)
	// Command builds the launch of a runtime for a port (default: the installed runtime, see rtm.Installer.Command).
	Command func(id string, port int) (proc.Spec, error)
	// Ready probes the server at a url (default: GET of the runtime's ready_path answers 200).
	Ready func(ctx context.Context, url string) error
	// StartTimeout and PollEvery tune the start (defaults: the catalog's start_timeout_sec, and proc's).
	StartTimeout time.Duration
	PollEvery    time.Duration
	Now          func() time.Time
}

// Queue is the part of the job queue the manager uses.
type Queue interface {
	Enqueue(ctx context.Context, kind domain.JobKind, payload any) (domain.Job, error)
	Register(kind domain.JobKind, handler jobqueue.Handler, resumable bool)
}

// instance is one runtime of the catalog with its place, record, installer and process.
type instance struct {
	rt      catalog.Runtime
	layout  store.RuntimeLayout
	state   *store.RuntimeStore
	install *rtm.Installer
	proc    *proc.Manager
	// work serializes the jobs of the runtime (two installs never write the same files).
	work sync.Mutex
}

// Service is the manager of the local runtimes.
type Service struct {
	Deps
	tracker   *track.Tracker
	instances []*instance

	gpuMu  sync.Mutex
	gpuRep gpu.Report
	gpuAt  time.Time

	mu      sync.Mutex
	active  map[string]string                  // target -> job id of the queued or running install
	cancels map[string]context.CancelCauseFunc // job id -> cancel of a running install
}

// New returns the Service. It reads the record of every runtime (a broken one is moved aside, see store.OpenRuntime).
func New(d Deps) (*Service, error) {
	if d.Logger == nil {
		d.Logger = slog.New(slog.DiscardHandler)
	}
	if d.Catalog == nil {
		d.Catalog = catalog.Default()
	}
	if d.Platform == (rtm.Platform{}) {
		d.Platform = rtm.Platform{OS: runtime.GOOS, Arch: runtime.GOARCH}
	}
	if d.GPU == nil {
		d.GPU = gpu.DetectSystem
	}
	if d.Extract == nil {
		d.Extract = extract.SevenZip{}
	}
	s := &Service{Deps: d, tracker: track.New(), active: map[string]string{}, cancels: map[string]context.CancelCauseFunc{}}
	for _, rt := range d.Catalog.Runtimes {
		inst, err := s.newInstance(rt)
		if err != nil {
			return nil, err
		}
		s.instances = append(s.instances, inst)
	}
	return s, nil
}

func (s *Service) newInstance(rt catalog.Runtime) (*instance, error) {
	layout := store.ForRuntime(s.Root, rt.ID)
	st, err := store.OpenRuntime(layout)
	if err != nil {
		return nil, fmt.Errorf("local runtime %s: %w", rt.ID, err)
	}
	inst := &instance{rt: rt, layout: layout, state: st}
	set, _ := s.Catalog.ResourceSet(rt.ID)
	inst.install = &rtm.Installer{Layout: layout, Runtime: rt, AssetSet: set, Store: st, Extract: s.Extract, Fetch: s.Fetch, FreeBytes: s.FreeBytes, Now: s.Now}
	build := inst.install.Command
	if s.Command != nil {
		build = func(port int) (proc.Spec, error) { return s.Command(rt.ID, port) }
	}
	ready := s.Ready
	if ready == nil {
		ready = proc.HTTPReady(rt.Launch.ReadyPath)
	}
	timeout := s.StartTimeout
	if timeout <= 0 {
		timeout = time.Duration(rt.Launch.StartTimeoutSec) * time.Second
	}
	inst.proc = proc.New(proc.Config{
		Name: rt.Name, Build: build, Ready: ready, StartTimeout: timeout, PollEvery: s.PollEvery, Logger: s.Logger,
		PreferredPort: func() int { return st.Get().LastPort },
	})
	return inst, nil
}

// instance finds the runtime with the id (ErrNotFound for another).
func (s *Service) instance(id string) (*instance, error) {
	for _, i := range s.instances {
		if i.rt.ID == id {
			return i, nil
		}
	}
	return nil, ErrNotFound
}

// gpuCacheTTL is how long a GPU report is reused by Status and List: the screen polls them, and a detection runs nvidia-smi.
const gpuCacheTTL = time.Minute

// report is the GPU report of the machine: the one of the last detection when it is younger than gpuCacheTTL and fresh is false.
// Install, Start and Prepare ask for a fresh one (their answer decides a download and a process).
func (s *Service) report(ctx context.Context, fresh bool) gpu.Report {
	s.gpuMu.Lock()
	defer s.gpuMu.Unlock()
	now := s.now()
	if !fresh && !s.gpuAt.IsZero() && now.Sub(s.gpuAt) < gpuCacheTTL {
		return s.gpuRep
	}
	s.gpuRep, s.gpuAt = s.GPU(ctx), now
	return s.gpuRep
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// support is what Evaluate says of this machine for the runtime. The GPU is listed only on the platform the runtime is for:
// elsewhere the answer does not depend on it.
func (s *Service) support(ctx context.Context, inst *instance, fresh bool) rtm.Support {
	var rep gpu.Report
	if s.Platform.OS == inst.rt.Support.OS && s.Platform.Arch == inst.rt.Support.Arch {
		rep = s.report(ctx, fresh)
	}
	return rtm.Evaluate(inst.rt, s.Platform, rep)
}

// refuse is the *SupportError of an unsupported machine, nil when the runtime can run here.
func (s *Service) refuse(ctx context.Context, inst *instance) (rtm.Support, error) {
	sup := s.support(ctx, inst, true)
	if !sup.Supported {
		return sup, &SupportError{Runtime: inst.rt.ID, Support: sup}
	}
	return sup, nil
}
