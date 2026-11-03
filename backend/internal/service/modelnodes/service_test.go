package modelnodes_test

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

	"github.com/stretchr/testify/assert"
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

const commit = "0123456789abcdef0123456789abcdef01234567"

// fakeEngine is the model manager's engine: a process that is up or down, and what the Service does to it, in order.
type fakeEngine struct {
	mu        sync.Mutex
	view      modelmanager.EngineView
	calls     []string
	enabledAt [][]string // the whitelist the engine would be started with, at each start
	nodes     *nodes.Manager
	startErr  error
}

func (e *fakeEngine) Engine() modelmanager.EngineView {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.view
}

func (e *fakeEngine) Catalog() []modelmanager.ModelView {
	return []modelmanager.ModelView{{ID: "uses-pack", Name: "Uses pack", State: modelmanager.ModelInstalled, RequiresNodes: []string{"pack"}}, {ID: "plain", Name: "Plain"}}
}

func (e *fakeEngine) StopEngine() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, "stop")
	e.view.Process.State = proc.StateStopped
	return nil
}

func (e *fakeEngine) StartEngine(context.Context) (proc.Status, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, "start")
	e.enabledAt = append(e.enabledAt, e.nodes.Enabled())
	if e.startErr != nil {
		return proc.Status{}, e.startErr
	}
	e.view.Process.State = proc.StateRunning
	return proc.Status{State: proc.StateRunning, URL: "http://127.0.0.1:1"}, nil
}

func (e *fakeEngine) Calls() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.calls...)
}

type rig struct {
	t      *testing.T
	svc    *modelnodes.Service
	engine *fakeEngine
	nodes  *nodes.Manager
	queue  *jobqueue.Queue
	node   nodecat.Node
	// loaded are the classes the fake engine loaded (nil: all of the node's).
	loaded  func(classes []string) []string
	started bool
}

func newRig(t *testing.T, edit func(*nodecat.Node)) *rig {
	t.Helper()
	files := map[string]string{"__init__.py": "NODE_CLASS_MAPPINGS = {}\n", "nodes.py": "x = 1\n"}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create("pack-" + commit + "/" + name)
		require.NoError(t, err)
		_, _ = w.Write([]byte(body))
	}
	require.NoError(t, zw.Close())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.ServeContent(w, req, "a.zip", time.Time{}, bytes.NewReader(buf.Bytes()))
	}))
	t.Cleanup(srv.Close)
	sum := sha256.Sum256(buf.Bytes())
	node := nodecat.Node{
		ID: "pack", Name: "Pack", Description: "d", Repo: "https://github.com/x/pack", Commit: commit, ArchiveURL: srv.URL + "/pack.zip",
		ArchiveSHA256: hex.EncodeToString(sum[:]), ArchiveSize: int64(buf.Len()), ExtractedSize: 1 << 10, Folder: "pack",
		License: catalog.License{ID: "gpl", Version: "3.0", Name: "GPL-3.0", Commercial: "yes"}, NodeClasses: []string{"PackLoader", "PackApply"},
	}
	if edit != nil {
		edit(&node)
	}
	r := &rig{t: t, node: node}
	r.nodes = nodes.New(store.Layout{Root: filepath.Join(t.TempDir(), "comfyui")})
	r.nodes.Catalog = &nodecat.Catalog{Nodes: []nodecat.Node{node}}
	anyURL := func(*url.URL) error { return nil }
	r.nodes.Fetch = fetch.Options{CheckURL: anyURL, Client: fetch.WithRedirectRules(&http.Client{}, anyURL, "")}
	r.nodes.FreeBytes = func(string) (uint64, error) { return 1 << 40, nil }
	r.engine = &fakeEngine{nodes: r.nodes}
	r.engine.view.Supported, r.engine.view.Writable = true, true
	r.engine.view.Installed = true
	table := repository.NewJobRepository(testdb.Open(t))
	r.queue = jobqueue.New(table, jobqueue.Options{Workers: 2})
	r.svc = modelnodes.New(modelnodes.Deps{
		Nodes: r.nodes, Engine: r.engine, Queue: r.queue, JobTable: table,
		Loaded: func(_ context.Context, _ string, classes []string) ([]string, error) {
			if r.loaded != nil {
				return r.loaded(classes), nil
			}
			return nil, nil
		},
	})
	r.queue.Register(domain.JobModelInstall, r.svc.Handler(func(context.Context, domain.Job) error {
		t.Error("a job of a node reached the next handler")
		return nil
	}), true)
	t.Cleanup(r.queue.Stop)
	return r
}

func (r *rig) accept() *modelnodes.LicenseRef {
	return &modelnodes.LicenseRef{ID: r.node.License.ID, Version: r.node.License.Version, NoticeVersion: modelnodes.NoticeVersion}
}

func (r *rig) wait(jobID string) modelnodes.JobStatus {
	r.t.Helper()
	if !r.started {
		r.started = true
		r.queue.Start(context.Background())
	}
	var st modelnodes.JobStatus
	require.Eventually(r.t, func() bool {
		var err error
		st, err = r.svc.JobStatus(context.Background(), jobID)
		return err == nil && (st.State == domain.JobSucceeded || st.State == domain.JobFailed)
	}, 30*time.Second, 20*time.Millisecond, "the job did not end")
	return st
}

func (r *rig) install() modelnodes.JobStatus {
	r.t.Helper()
	ref, err := r.svc.Install(context.Background(), "pack", r.accept())
	require.NoError(r.t, err)
	return r.wait(ref.JobID)
}

func TestInstallRefusesBeforeAnythingIsFetched(t *testing.T) {
	var invalid *modelmanager.InvalidError
	t.Run("not accepted", func(t *testing.T) {
		r := newRig(t, nil)
		_, err := r.svc.Install(context.Background(), "pack", nil)
		require.ErrorAs(t, err, &invalid)
		assert.Contains(t, invalid.Message, "同意してから")
		assert.Empty(t, r.nodes.Enabled())
	})
	t.Run("an acceptance of an older notice", func(t *testing.T) {
		r := newRig(t, nil)
		ref := r.accept()
		ref.NoticeVersion = "old"
		_, err := r.svc.Install(context.Background(), "pack", ref)
		require.ErrorAs(t, err, &invalid)
		assert.Contains(t, invalid.Message, "違います")
	})
	t.Run("blocked", func(t *testing.T) {
		r := newRig(t, func(n *nodecat.Node) { n.InstallBlocked = "needs opencv" })
		_, err := r.svc.Install(context.Background(), "pack", r.accept())
		require.ErrorAs(t, err, &invalid)
		assert.Contains(t, invalid.Message, "needs opencv")
	})
	t.Run("unknown", func(t *testing.T) {
		r := newRig(t, nil)
		_, err := r.svc.Install(context.Background(), "nope", nil)
		require.ErrorIs(t, err, modelmanager.ErrNotFound)
	})
	t.Run("no bundled engine on this OS", func(t *testing.T) {
		r := newRig(t, nil)
		r.engine.view.Supported = false
		_, err := r.svc.Install(context.Background(), "pack", r.accept())
		require.ErrorIs(t, err, modelmanager.ErrUnsupported)
	})
	t.Run("the engine is not installed", func(t *testing.T) {
		r := newRig(t, nil)
		r.engine.view.Installed = false
		_, err := r.svc.Install(context.Background(), "pack", r.accept())
		require.ErrorIs(t, err, modelmanager.ErrEngineNotInstalled)
	})
	t.Run("the folder cannot be written to", func(t *testing.T) {
		r := newRig(t, nil)
		r.engine.view.Writable = false
		_, err := r.svc.Install(context.Background(), "pack", r.accept())
		require.ErrorAs(t, err, &invalid)
		assert.Contains(t, invalid.Message, "書き込めない")
	})
	t.Run("no room", func(t *testing.T) {
		r := newRig(t, nil)
		r.nodes.FreeBytes = func(string) (uint64, error) { return 10, nil }
		_, err := r.svc.Install(context.Background(), "pack", r.accept())
		var ns *fetch.NoSpaceError
		require.ErrorAs(t, err, &ns)
	})
}

// A stopped engine is not started by the install: the node is read at its next start, and the card says so.
func TestInstallWhileTheEngineIsStoppedOnlyPutsTheNodeInPlace(t *testing.T) {
	r := newRig(t, nil)
	st := r.install()
	require.Equal(t, domain.JobSucceeded, st.State, st.Error)
	assert.Empty(t, r.engine.Calls(), "the engine was not touched")
	assert.Equal(t, []string{"pack"}, r.nodes.Enabled())
	assert.Contains(t, st.Progress.Message, "次に ComfyUI を起動したとき")

	v, err := r.svc.Status(context.Background(), "pack")
	require.NoError(t, err)
	assert.Equal(t, nodes.StateInstalled, v.State)
	assert.True(t, v.Accepted)
	assert.Equal(t, []modelnodes.ModelRef{{ID: "uses-pack", Name: "Uses pack", Installed: true}}, v.Models, "the models that need the node are listed")
}

// A running engine is stopped for the swap of the folder, started again with the node in its whitelist, and asked for the node's classes.
func TestInstallRestartsARunningEngineWithTheNodeWhitelisted(t *testing.T) {
	r := newRig(t, nil)
	r.engine.view.Process.State = proc.StateRunning
	st := r.install()
	require.Equal(t, domain.JobSucceeded, st.State, st.Error)
	assert.Equal(t, []string{"stop", "start"}, r.engine.Calls())
	assert.Equal(t, [][]string{{"pack"}}, r.engine.enabledAt, "the engine starts with the node in its whitelist")
}

// "installed" means "loaded at the install": a node ComfyUI did not load is taken out again, and the engine runs without it.
func TestANodeTheEngineDidNotLoadIsTakenOutAgain(t *testing.T) {
	r := newRig(t, nil)
	r.engine.view.Process.State = proc.StateRunning
	r.loaded = func([]string) []string { return []string{"PackApply"} }

	st := r.install()

	require.Equal(t, domain.JobFailed, st.State)
	assert.Contains(t, st.Error, "PackApply")
	assert.Contains(t, st.Error, "取り消しました")
	assert.Empty(t, r.nodes.Enabled())
	v, err := r.svc.Status(context.Background(), "pack")
	require.NoError(t, err)
	assert.Equal(t, nodes.StateNotInstalled, v.State)
	assert.Equal(t, []string{"stop", "start", "stop", "start"}, r.engine.Calls(), "the engine is started again without the node")
	assert.Equal(t, []string{"pack"}, r.engine.enabledAt[0])
	assert.Empty(t, r.engine.enabledAt[1])
}

func TestAFailedInstallStartsARunningEngineAgainAndInstallsNothing(t *testing.T) {
	r := newRig(t, func(n *nodecat.Node) {
		n.ArchiveSHA256 = "0000000000000000000000000000000000000000000000000000000000000000"
	})
	r.engine.view.Process.State = proc.StateRunning
	st := r.install()
	require.Equal(t, domain.JobFailed, st.State)
	assert.Contains(t, st.Error, "SHA-256")
	assert.Empty(t, r.nodes.Enabled())
	assert.Equal(t, []string{"start"}, r.engine.Calls(), "the engine was running, was not stopped yet (the failure came before the swap), and is started to be sure")
}

func TestRemoveStopsTheEngineAndDeletesTheFolder(t *testing.T) {
	r := newRig(t, nil)
	require.Equal(t, domain.JobSucceeded, r.install().State)
	r.engine.view.Process.State = proc.StateRunning
	r.engine.calls = nil

	require.NoError(t, r.svc.Remove(context.Background(), "pack"))

	assert.Empty(t, r.nodes.Enabled())
	assert.NoDirExists(t, r.nodes.Folder(r.node))
	assert.Equal(t, []string{"stop", "start"}, r.engine.Calls())
	assert.Empty(t, r.engine.enabledAt[len(r.engine.enabledAt)-1], "the engine is started without the node")
	require.ErrorIs(t, r.svc.Remove(context.Background(), "pack"), modelmanager.ErrNotInstalled)
	require.ErrorIs(t, r.svc.Remove(context.Background(), "nope"), modelmanager.ErrNotFound)
}

func TestListShowsBlockedNodesWithTheirReasonAndWhyNothingCanBeInstalled(t *testing.T) {
	r := newRig(t, func(n *nodecat.Node) { n.InstallBlocked = "needs opencv" })
	r.engine.view.Installed = false
	views := r.svc.List(context.Background())
	require.Len(t, views, 1)
	assert.Equal(t, nodes.StateBlocked, views[0].State)
	assert.Equal(t, "needs opencv", views[0].InstallBlocked)
	assert.Contains(t, views[0].Unavailable, "ComfyUI エンジン")
	assert.Equal(t, "GPL-3.0", views[0].License.Name)
}
