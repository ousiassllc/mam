package ollamatest

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/ollamamodels"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	rtm "github.com/ousiassllc/mam/backend/internal/modelmgr/runtime"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
	"github.com/ousiassllc/mam/backend/internal/service/ollamapull"
)

// Runtime is the fake runtime manager (ollamapull.Runtime): the Ollama of mam is supported or not, installed or not, running or stopped; it starts at Prepare and says what was done to it.
type Runtime struct {
	mu sync.Mutex
	// Supported, Installed and InstallJob are the state of the runtime; State the process; Reason the reason of an unsupported machine; LogTail the end of its log.
	Supported, Installed bool
	InstallJob           string
	State                proc.State
	Reason               string
	LogTail              string
	// PrepareErr is why a start fails.
	PrepareErr error
	url        string
	calls      []string
}

// Status implements ollamapull.Runtime.
func (r *Runtime) Status(context.Context, string) (localruntime.View, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v := localruntime.View{ID: ollamapull.RuntimeID, InstallJob: r.InstallJob, LogTail: r.LogTail, FreeBytes: 1 << 40}
	v.Support = rtm.Support{Supported: r.Supported, Reason: r.Reason, Fallback: "「別のサーバー」につないでください"}
	v.Installed, v.Weights.State = r.Installed, localruntime.WeightsReady
	v.Process = proc.Status{State: r.State}
	if r.State == proc.StateRunning {
		v.Process.URL = r.url
	}
	return v, nil
}

// Prepare implements ollamapull.Runtime: a stopped runtime runs.
func (r *Runtime) Prepare(context.Context, string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, "prepare")
	if r.PrepareErr != nil {
		r.State = proc.StateFailed
		return r.PrepareErr
	}
	r.State = proc.StateRunning
	return nil
}

// Stop implements ollamapull.Runtime.
func (r *Runtime) Stop(string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, "stop")
	r.State = proc.StateStopped
	return nil
}

// Calls are what the service asked the runtime to do ("prepare", "stop"), in order.
func (r *Runtime) Calls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

// Token implements llamaserver.Runtime: the Ollama of mam has no token.
func (r *Runtime) Token(string) (string, error) { return "", nil }

// SetState sets the process state.
func (r *Runtime) SetState(s proc.State) {
	r.mu.Lock()
	r.State = s
	r.mu.Unlock()
}

// Rig is the service with its fakes.
type Rig struct {
	t       *testing.T
	Svc     *ollamapull.Service
	Runtime *Runtime
	Fake    *Fake
	Queue   *jobqueue.Queue
	Layout  store.RuntimeLayout
	// Free is the free space the service sees; UsedBy the Agents whose assignment chooses a model (by name).
	mu      sync.Mutex
	free    uint64
	usedBy  map[string][]string
	started bool
	shared  bool
	// Others are the jobs that were not a pull's.
	Others chan domain.Job
}

// Option changes the Deps before the Service is made.
type Option func(*Rig, *ollamapull.Deps)

// Free sets the free disk space the service sees.
func Free(n uint64) Option { return func(r *Rig, _ *ollamapull.Deps) { r.free = n } }

// Shared puts the jobs on a queue and table that exist already (for a test of a layer above that reads them through its own service). The queue is started by its owner, and the handler is not
// registered: the owner chains Svc.Handler into the handler of model_install it registers.
func Shared(q *jobqueue.Queue, table *repository.JobRepository) Option {
	return func(r *Rig, d *ollamapull.Deps) {
		r.Queue, r.started, r.shared, d.JobTable = q, true, true, table
	}
}

// WithModels replaces the list the service may fetch.
func WithModels(m ...ollamamodels.Model) Option {
	return func(_ *Rig, d *ollamapull.Deps) { d.Models = m }
}

// New builds the rig: the Ollama of mam is supported, installed and stopped, there is much free space, no model is fetched. The queue is started by Wait.
func New(t *testing.T, opts ...Option) *Rig {
	t.Helper()
	r := &Rig{t: t, free: 1 << 40, usedBy: map[string][]string{}, Others: make(chan domain.Job, 8)}
	root := filepath.Join(t.TempDir(), "install", "localai")
	r.Layout = store.ForRuntime(root, ollamapull.RuntimeID)
	r.Fake = newFake(t, r.Layout.ModelsDir())
	r.Runtime = &Runtime{Supported: true, Installed: true, State: proc.StateStopped, url: r.Fake.URL}
	d := ollamapull.Deps{
		Root: root, Runtime: r.Runtime, HTTP: r.Fake.Client(),
		FreeBytes: func(string) (uint64, error) { r.mu.Lock(); defer r.mu.Unlock(); return r.free, nil },
		UsedBy:    func(model string) []string { r.mu.Lock(); defer r.mu.Unlock(); return r.usedBy[model] },
	}
	for _, o := range opts {
		o(r, &d)
	}
	if r.Queue == nil {
		d.JobTable = repository.NewJobRepository(testdb.Open(t))
		r.Queue = jobqueue.New(d.JobTable, jobqueue.Options{Workers: 2})
		t.Cleanup(r.Queue.Stop)
	}
	r.Fake.Models = d.Models
	d.Queue = r.Queue
	r.Svc = ollamapull.New(d)
	if !r.shared {
		r.Queue.Register(domain.JobModelInstall, r.Svc.Handler(func(_ context.Context, job domain.Job) error { r.Others <- job; return nil }), true)
	}
	return r
}

// Use makes the assignment of the Agents name model: the delete of that model is refused.
func (r *Rig) Use(model string, agents ...string) {
	r.mu.Lock()
	r.usedBy[model] = agents
	r.mu.Unlock()
}

// SetFree changes the free space.
func (r *Rig) SetFree(n uint64) {
	r.mu.Lock()
	r.free = n
	r.mu.Unlock()
}

// Wait starts the queue (once) and waits until the job ended.
func (r *Rig) Wait(jobID string) ollamapull.JobStatus {
	r.t.Helper()
	r.Start()
	var st ollamapull.JobStatus
	require.Eventually(r.t, func() bool {
		var err error
		st, err = r.Svc.JobStatus(context.Background(), jobID)
		return err == nil && (st.State == domain.JobSucceeded || st.State == domain.JobFailed)
	}, 30*time.Second, 20*time.Millisecond, "the job did not end")
	return st
}

// Start starts the queue (once).
func (r *Rig) Start() {
	if !r.started {
		r.started = true
		r.Queue.Start(context.Background())
	}
}
