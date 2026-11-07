// Package ollamapull is how mam fetches the models of the Ollama that it manages (the local runtime "ollama", spec/architecture/model-manager/local-runtime.md §19): the short list of
// models of the catalog (catalog.OllamaModels), their state, the fetch (a model_install job with the target "ollama:<name>:<tag>" that streams the NDJSON of Ollama's own POST /api/pull),
// the check of the manifest Ollama wrote against the record of the catalog, the cancel and the delete (DELETE /api/delete).
//
// The models are fetched by Ollama, not by mam's downloader: a model of Ollama is several blobs and a manifest in a layout Ollama owns (its resume, its de-duplication, the SHA-256 of every
// layer it checks itself), and writing that layout from outside would tie mam to its inner format. What mam holds is the list of names and the comparison afterwards. A tag of the registry
// can be moved, so mam cannot pin what a pull brings (weaker than the commit-pinned models of llama.cpp and ComfyUI): a manifest that differs from the record is "changed", which still works.
//
// Nothing outside the list is fetched: a name that is not "<name>:<tag>" of the library, has a cloud tag (a model that runs on the servers of Ollama, which would send the prompt out) or is
// not in the catalog is refused before any request is made. Like modelmanager it does not import package service: its refusals are *InvalidError and the Err values of modelmanager and
// localruntime, which the screens and the MCP tools already map.
package ollamapull

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/httpclient"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/ollamamodels"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/track"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

// RuntimeID is the Ollama runtime of the catalog (data/runtimes/ollama.json); TargetPrefix starts the target of a job of a model ("ollama:gemma4:12b").
const (
	RuntimeID    = "ollama"
	TargetPrefix = "ollama:"
)

// spaceMargin is the room that is left on the disk besides the model (the spec: the total and 1 GiB).
const spaceMargin = 1 << 30

// InvalidError is a refused input; Message is shown to the user.
type InvalidError = modelmanager.InvalidError

// Runtime is the part of *localruntime.Service the service uses.
type Runtime interface {
	Status(ctx context.Context, id string) (localruntime.View, error)
	// Prepare starts the runtime when it is stopped and waits until it answers.
	Prepare(ctx context.Context, id string) error
	Stop(id string) error
}

// Queue is the part of the job queue the service uses.
type Queue interface {
	Enqueue(ctx context.Context, kind domain.JobKind, payload any) (domain.Job, error)
}

// Deps are what the Service needs.
type Deps struct {
	// Root is the localai directory next to mam.exe (store.LocalAIRoot): the models are in its ollama\models.
	Root string
	// Runtime is the manager of the local runtimes; the app sets it after it built it (the job handler is registered first: the runtimes wrap it).
	Runtime Runtime
	// Queue and JobTable are the queue the jobs run on and its table (read for their state).
	Queue    Queue
	JobTable *repository.JobRepository
	// Models are the models that may be fetched (nil: catalog.Default().OllamaModels).
	Models []ollamamodels.Model
	// UsedBy names the Agents whose LLM assignment (provider "local") chooses the model, for a delete to refuse (nil: none does).
	UsedBy func(model string) []string
	// HTTP sends the requests to the server (nil: the client of package httpclient). FreeBytes reports the free disk space at a path (nil: fsutil.FreeBytes).
	HTTP      httpclient.Doer
	FreeBytes func(path string) (uint64, error)
	Logger    *slog.Logger
	Now       func() time.Time
}

// Service is the model fetcher of the Ollama of mam.
type Service struct {
	Deps
	layout  store.RuntimeLayout
	tracker *track.Tracker

	mu      sync.Mutex
	active  map[string]string // target -> job id of the queued or running pull
	cancels map[string]context.CancelCauseFunc
}

// New returns the Service.
func New(d Deps) *Service {
	if d.Logger == nil {
		d.Logger = slog.New(slog.DiscardHandler)
	}
	if d.Models == nil {
		d.Models = catalog.Default().OllamaModels
	}
	if d.HTTP == nil {
		d.HTTP = httpclient.New()
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	return &Service{Deps: d, layout: store.ForRuntime(d.Root, RuntimeID), tracker: track.New(), active: map[string]string{}, cancels: map[string]context.CancelCauseFunc{}}
}

// Catalog is the models that may be fetched, in the order of the screen.
func (s *Service) Catalog() []ollamamodels.Model { return s.Models }

// find is the model of the catalog called name: a name that may not be fetched at all, or is not in the list, is an *InvalidError.
func (s *Service) find(name string) (ollamamodels.Model, error) {
	if err := ollamamodels.CheckName(name); err != nil {
		return ollamamodels.Model{}, &InvalidError{Message: "取得できるのは、Ollama の公式のモデル（library の <名前>:<タグ>。クラウドのモデルを除く）だけです"}
	}
	m, ok := ollamamodels.Find(s.Models, name)
	if !ok {
		return ollamamodels.Model{}, &InvalidError{Message: "そのモデルは、mam が取得できるモデルの一覧にありません"}
	}
	return m, nil
}

// Watch calls fn with every change of the progress of a pull, until cancel is called. fn must not block.
func (s *Service) Watch(fn func(track.Entry)) (cancel func()) { return s.tracker.Subscribe(fn) }

func (s *Service) activeJob(target string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active[target]
}

// Pulling names the models whose pull is queued or running, in the order of the list: the Ollama of mam is held while there is one (localruntime.Deps.HeldBy),
// so that it is neither removed nor replaced nor stopped under the fetch.
func (s *Service) Pulling() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var names []string
	for _, m := range s.Models {
		if s.active[TargetPrefix+m.Name] != "" {
			names = append(names, m.Name)
		}
	}
	return names
}

// HeldBy is localruntime.Deps.HeldBy: the Ollama of mam is held by the models being fetched into it ("gemma4:12b、qwen3:8b"), no other runtime by anything.
func (s *Service) HeldBy(id string) string {
	if id != RuntimeID {
		return ""
	}
	return strings.Join(s.Pulling(), "、")
}

func (s *Service) release(target, jobID string) {
	s.mu.Lock()
	if s.active[target] == jobID {
		delete(s.active, target)
	}
	s.mu.Unlock()
}

// processURL is the address of the running server ("" while it does not run).
func processURL(v localruntime.View) string {
	if v.Process.State != proc.StateRunning {
		return ""
	}
	return v.Process.URL
}
