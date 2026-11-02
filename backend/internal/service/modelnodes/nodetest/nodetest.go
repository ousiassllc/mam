// Package nodetest is a modelnodes.Service over fakes, for the tests of the layers above it (the screen's handlers, the MCP tools): a fake host
// that serves the archive of a node, a fake bundled engine, and a temporary directory for the nodes. The allowlist is two entries: "pack",
// which installs, and "needs-pip", which mam does not install. Not for production code.
package nodetest

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/nodecat"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/nodes"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelnodes"
)

// Commit is the commit the node "pack" is pinned to.
const Commit = "0123456789abcdef0123456789abcdef01234567"

// Engine is the fake bundled engine of the service: it is up or down, and records what the service did to it.
type Engine struct {
	mu    sync.Mutex
	View  modelmanager.EngineView
	Calls []string
	nodes *nodes.Manager
	// Needing are the models of the catalog that need "pack".
	Needing []modelmanager.ModelView
}

// Engine implements modelnodes.Engine.
func (e *Engine) Engine() modelmanager.EngineView {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.View
}

// Catalog implements modelnodes.Engine.
func (e *Engine) Catalog() []modelmanager.ModelView { return e.Needing }

// StopEngine implements modelnodes.Engine.
func (e *Engine) StopEngine() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.Calls = append(e.Calls, "stop")
	e.View.Process.State = proc.StateStopped
	return nil
}

// StartEngine implements modelnodes.Engine.
func (e *Engine) StartEngine(context.Context) (proc.Status, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.Calls = append(e.Calls, "start")
	e.View.Process.State = proc.StateRunning
	return proc.Status{State: proc.StateRunning, URL: "http://127.0.0.1:1"}, nil
}

// Rig is the service with its fakes.
type Rig struct {
	t       *testing.T
	Svc     *modelnodes.Service
	Engine  *Engine
	Nodes   *nodes.Manager
	Queue   *jobqueue.Queue
	started bool
}

// Option changes the rig.
type Option func(*options)

type options struct {
	queue *jobqueue.Queue
	table *repository.JobRepository
	next  jobqueue.Handler
}

// Shared puts the jobs of the nodes on a queue and table that exist already (the model manager's, for a test that reads them through get_job);
// next takes every job that is not a node's (the model manager's handler).
func Shared(queue *jobqueue.Queue, table *repository.JobRepository, next jobqueue.Handler) Option {
	return func(o *options) { o.queue, o.table, o.next = queue, table, next }
}

// New builds the rig. The engine is installed, writable and stopped; "pack" has a catalog model that needs it.
func New(t *testing.T, opts ...Option) *Rig { return NewFor(t, "pack", opts...) }

// NewFor is New with the installable node named id (the ID of a node a template requires, such as "ipadapter-plus"): the catalog model
// of the engine fake needs it.
func NewFor(t *testing.T, id string, opts ...Option) *Rig {
	t.Helper()
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range map[string]string{"__init__.py": "NODE_CLASS_MAPPINGS = {}\n", "nodes.py": "x = 1\n"} {
		w, err := zw.Create("pack-" + Commit + "/" + name)
		require.NoError(t, err)
		_, _ = w.Write([]byte(body))
	}
	require.NoError(t, zw.Close())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.ServeContent(w, req, "a.zip", time.Time{}, bytes.NewReader(buf.Bytes()))
	}))
	t.Cleanup(srv.Close)
	sum := sha256.Sum256(buf.Bytes())
	pack := nodecat.Node{
		ID: id, Name: "Pack", Description: "A pack of nodes.", Repo: "https://github.com/x/pack", Commit: Commit, ArchiveURL: srv.URL + "/" + id + ".zip",
		ArchiveSHA256: hex.EncodeToString(sum[:]), ArchiveSize: int64(buf.Len()), ExtractedSize: 1 << 10, Folder: id,
		License:        catalog.License{ID: "gpl", Version: "3.0", Name: "GPL-3.0", URL: "https://example.com/gpl", Summary: "Copyleft.", Commercial: "yes", CheckedAt: "2026-11-03"},
		PythonRequires: []string{"torch"}, NodeClasses: []string{"PackLoader"}, ReviewedAt: "2026-11-03", Review: "No network, no subprocess.",
	}
	blocked := pack
	blocked.ID, blocked.Name, blocked.Folder = "needs-pip", "Needs pip", "needs-pip"
	blocked.InstallBlocked = "needs opencv, which the bundled Python does not have"
	blocked.License.Commercial = "unknown"

	r := &Rig{t: t}
	r.Nodes = nodes.New(store.Layout{Root: filepath.Join(t.TempDir(), "comfyui")})
	r.Nodes.Catalog = &nodecat.Catalog{Nodes: []nodecat.Node{pack, blocked}}
	anyURL := func(*url.URL) error { return nil }
	r.Nodes.Fetch = fetch.Options{CheckURL: anyURL, Client: fetch.WithRedirectRules(&http.Client{}, anyURL, "")}
	r.Nodes.FreeBytes = func(string) (uint64, error) { return 1 << 40, nil }
	r.Engine = &Engine{nodes: r.Nodes, Needing: []modelmanager.ModelView{{ID: "uses-pack", Name: "Uses pack", RequiresNodes: []string{id}}}}
	r.Engine.View.Supported, r.Engine.View.Writable, r.Engine.View.Installed = true, true, true
	table, next := o.table, o.next
	if o.queue != nil {
		r.Queue, r.started = o.queue, true // the owner of the queue starts it
	} else {
		table = repository.NewJobRepository(testdb.Open(t))
		r.Queue = jobqueue.New(table, jobqueue.Options{Workers: 2})
		next = func(context.Context, domain.Job) error {
			t.Error("a job of a node reached the next handler")
			return nil
		}
		t.Cleanup(r.Queue.Stop)
	}
	r.Svc = modelnodes.New(modelnodes.Deps{
		Nodes: r.Nodes, Engine: r.Engine, Queue: r.Queue, JobTable: table,
		Loaded: func(context.Context, string, []string) ([]string, error) { return nil, nil },
	})
	r.Queue.Register(domain.JobModelInstall, r.Svc.Handler(next), true)
	return r
}

// Accept is the acceptance of the license and the security notice of "pack", as the consent form sends it.
func (r *Rig) Accept() *modelnodes.LicenseRef {
	return &modelnodes.LicenseRef{ID: "gpl", Version: "3.0", NoticeVersion: modelnodes.NoticeVersion}
}

// Wait starts the queue (once) and waits until the job ended.
func (r *Rig) Wait(jobID string) modelnodes.JobStatus {
	r.t.Helper()
	if !r.started {
		r.started = true
		r.Queue.Start(context.Background())
	}
	var st modelnodes.JobStatus
	require.Eventually(r.t, func() bool {
		var err error
		st, err = r.Svc.JobStatus(context.Background(), jobID)
		return err == nil && (st.State == domain.JobSucceeded || st.State == domain.JobFailed)
	}, 30*time.Second, 20*time.Millisecond, "the job did not end")
	return st
}
