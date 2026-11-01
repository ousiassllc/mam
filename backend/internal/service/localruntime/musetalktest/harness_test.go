// Package musetalktest tests the lip-sync side of localruntime.Service (the MuseTalk runtime: install from its parts, start, stop, and
// Service.LipSync) over fakes: a fake host that serves the parts and the weight, and a fake lip-sync server that is the test binary
// itself (the contract of mam_musetalk_server.py, spec/architecture/model-manager/local-runtime.md §13.3). The directory of the
// localruntime package is at its line budget, so these tests have a directory of their own.
package musetalktest_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/gpu"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/lipsync"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	rtm "github.com/ousiassllc/mam/backend/internal/modelmgr/runtime"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
)

// TestMain turns the test binary into the fake lip-sync server when MAM_FAKE_RUNTIME is set (it listens on FAKE_PORT).
func TestMain(m *testing.M) {
	if os.Getenv("MAM_FAKE_RUNTIME") == "" {
		os.Exit(m.Run())
	}
	serveFakeMuseTalk()
}

func ctx() context.Context { return context.Background() }

func sum256(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// serveFakeMuseTalk is the lip-sync server of these tests (the test binary started with MAM_FAKE_RUNTIME): the contract of
// musetalk_server.py (spec §13.3) with a canned engine that answers by the name of the video.
func serveFakeMuseTalk() {
	l, err := net.Listen("tcp", "127.0.0.1:"+os.Getenv("FAKE_PORT"))
	if err != nil {
		os.Exit(4)
	}
	token := os.Getenv("MAM_RUNTIME_TOKEN")
	fail := func(w http.ResponseWriter, status int, code, msg string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": msg}})
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"status":"ok"}`)) })
	mux.HandleFunc(lipsync.Path, func(w http.ResponseWriter, r *http.Request) {
		if len(token) < 16 || r.Header.Get(lipsync.TokenHeader) != token {
			fail(w, http.StatusForbidden, "forbidden", "トークンが一致しません")
			return
		}
		var req lipsync.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			fail(w, http.StatusBadRequest, "bad_request", "JSON ではありません")
			return
		}
		switch filepath.Base(req.VideoPath) {
		case "noface.mp4":
			fail(w, http.StatusUnprocessableEntity, "no_face", "動画から顔を見つけられませんでした")
			return
		case "oom.mp4":
			fail(w, http.StatusServiceUnavailable, "out_of_memory", "ビデオメモリが足りません")
			return
		}
		if err := os.WriteFile(req.OutputPath, []byte("fake-mp4"), 0o600); err != nil {
			fail(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		_ = json.NewEncoder(w).Encode(lipsync.Response{OutputPath: req.OutputPath, Frames: 25, FPS: 25, DurationSec: 1, ElapsedSec: 0.5})
	})
	_ = (&http.Server{Handler: mux, ReadHeaderTimeout: time.Minute}).Serve(l)
}

// host is the fake host of the parts and the weight (with Range, as the real ones).
type host struct{ *httptest.Server }

func newHost(t *testing.T, files map[string][]byte) *host {
	t.Helper()
	h := &host{httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, "f", time.Time{}, bytes.NewReader(body))
	}))}
	t.Cleanup(h.Close)
	return h
}

// rig is the Service over the fakes: a Windows x64 machine with an RTX 4070 unless an option says another.
type rig struct {
	t      *testing.T
	svc    *localruntime.Service
	root   string
	layout store.RuntimeLayout
	queue  *jobqueue.Queue
	table  *repository.JobRepository
	gpu    gpu.Report
	// started: the queue runs.
	started bool
}

type option func(*rig, *localruntime.Deps)

func platform(os, arch string) option {
	return func(_ *rig, d *localruntime.Deps) { d.Platform = rtm.Platform{OS: os, Arch: arch} }
}

func withGPU(a gpu.Adapter) option {
	return func(r *rig, _ *localruntime.Deps) { r.gpu = gpu.Report{Adapters: []gpu.Adapter{a}} }
}

func rtx(name string, vramGiB uint64, cc float64) gpu.Adapter {
	return gpu.Adapter{Name: name, VendorID: gpu.VendorNVIDIA, VRAMBytes: vramGiB << 30, ComputeCap: cc}
}

func newRig(t *testing.T, opts ...option) *rig {
	t.Helper()
	r := &rig{t: t, gpu: gpu.Report{Adapters: []gpu.Adapter{rtx("NVIDIA GeForce RTX 4070", 12, 8.9)}}}
	r.root = filepath.Join(t.TempDir(), "install", "localai")
	r.layout = store.ForRuntime(r.root, "musetalk")
	r.table = repository.NewJobRepository(testdb.Open(t))
	r.queue = jobqueue.New(r.table, jobqueue.Options{Workers: 2})
	anyURL := func(*url.URL) error { return nil }
	d := localruntime.Deps{
		Root: r.root, Catalog: museTalkCatalog(t), Queue: r.queue, JobTable: r.table,
		Platform:  rtm.Platform{OS: "windows", Arch: "amd64"},
		GPU:       func(context.Context) gpu.Report { return r.gpu },
		FreeBytes: func(string) (uint64, error) { return 1 << 40, nil },
		Fetch:     fetch.Options{CheckURL: anyURL, Client: fetch.WithRedirectRules(&http.Client{}, anyURL, "")},
		Command: func(_ string, port int) (proc.Spec, error) {
			return proc.Spec{Command: os.Args[0], LogPath: r.layout.LogPath(), Env: []string{"MAM_FAKE_RUNTIME=1", "FAKE_PORT=" + strconv.Itoa(port)}}, nil
		},
		PollEvery: 20 * time.Millisecond, StartTimeout: 20 * time.Second,
	}
	for _, o := range opts {
		o(r, &d)
	}
	var err error
	r.svc, err = localruntime.New(d)
	require.NoError(t, err)
	r.svc.Register(func(context.Context, domain.Job) error { return nil })
	t.Cleanup(func() { r.queue.Stop(); r.svc.Close() })
	return r
}

func (r *rig) installMuseTalk() {
	r.t.Helper()
	ref, err := r.svc.Install(ctx(), "musetalk")
	require.NoError(r.t, err)
	if !r.started {
		r.started = true
		r.queue.Start(ctx())
	}
	var st localruntime.JobStatus
	require.Eventually(r.t, func() bool {
		st, err = r.svc.JobStatus(ctx(), ref.JobID)
		return err == nil && (st.State == domain.JobSucceeded || st.State == domain.JobFailed)
	}, 30*time.Second, 20*time.Millisecond, "the install job did not end")
	require.Equal(r.t, domain.JobSucceeded, st.State, st.Error)
}
