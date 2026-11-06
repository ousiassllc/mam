// Package screentest drives the model manager screen over a real Service (a fake engine process and a fake Hugging Face) as
// the browser's HTMX does: the CSRF cookie and header, HX-Request. It is for the tests of the handler packages.
package screentest

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/jobrecent"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/router/settingsroutes"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/facemodels/facemodelstest"
	"github.com/ousiassllc/mam/backend/internal/service/joblist"
	"github.com/ousiassllc/mam/backend/internal/service/llmlocal"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime/lrtest"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/service/modelnodes/nodetest"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
	"github.com/ousiassllc/mam/backend/internal/worktrack"
)

// NodeCommitForTest is the commit the node "pack" of NewWithNodes is pinned to.
const NodeCommitForTest = nodetest.Commit

// Screen is the screen under test.
type Screen struct {
	T   *testing.T
	Rig *mmtest.Rig
	// Svc is the service layer behind the screen (to create a Project the pages of the app can be opened for).
	Svc *service.Services
	// Rts are the local runtimes (NewWithRuntimes); nil for a screen without them.
	Rts *lrtest.Rig
	// Nodes are the custom nodes (NewWithNodes); nil for a screen without them.
	Nodes *nodetest.Rig
	// LLM is the local LLM server of the page "ローカル LLM" (llm.json at LLMFile; its keys are in LLMKeys). It has no server of mam: a test sets them with SetManaged / SetManagedOllama.
	LLM     *llmlocal.Service
	LLMFile string
	LLMKeys *credentialtest.Memory
	// Works are the works that have no row in the job table (worktrack), which the job list reads next to the table.
	Works  *worktrack.Tracker
	engine http.Handler
	cookie *http.Cookie
	token  string
}

// New opens the screen: it loads the page for the CSRF cookie and token.
func New(t *testing.T, opts ...mmtest.Option) *Screen {
	t.Helper()
	return open(t, mmtest.New(t, opts...), nil, nil, false)
}

// NewWithLLM opens the screen with the local LLM server over the key store LLMKeys; pinned says the operator named the settings file (--llm-config), which the screen must not write.
// The runtimes have the llama.cpp of lrtest.WithLLMRuntime, so that the page "ローカル LLM" has its cards.
func NewWithLLM(t *testing.T, pinned bool) *Screen {
	t.Helper()
	rig := mmtest.New(t)
	return open(t, rig, lrtest.New(t, rig.Queue, rig.Table, rig.Svc.HandleInstall, lrtest.WithLLMRuntime()), nil, pinned)
}

// NewWithRuntimes opens the screen with the local runtimes over fakes (a fake package and weights, a fake process; a Windows PC with an RTX
// 4070 unless an option says another), on the queue of the model manager.
func NewWithRuntimes(t *testing.T, opts ...lrtest.Option) *Screen {
	t.Helper()
	rig := mmtest.New(t)
	return open(t, rig, lrtest.New(t, rig.Queue, rig.Table, rig.Svc.HandleInstall, opts...), nil, false)
}

// NewWithNodes opens the screen with the custom nodes of the allowlist over fakes (see nodetest): a fake host for the archive and a fake bundled engine.
func NewWithNodes(t *testing.T) *Screen {
	t.Helper()
	nodes := nodetest.New(t)
	s := open(t, mmtest.New(t), nil, nodes, false)
	s.Nodes = nodes
	return s
}

// NewAll opens the screen with every category: the local runtimes, the custom nodes, the face-check models and the companion software (each over fakes of its own;
// the actions of the runtimes and the nodes are not run through this screen, only their pages).
func NewAll(t *testing.T) *Screen {
	t.Helper()
	rig := mmtest.New(t)
	rig.Gentest.FaceModels = facemodelstest.New(t).Svc
	nodes := nodetest.New(t)
	ffmpeg, _ := IsolatedFFmpeg(t)
	faceFusion, _ := IsolatedFaceFusion(t)
	s := open(t, rig, lrtest.New(t, rig.Queue, rig.Table, rig.Svc.HandleInstall, lrtest.WithLLMRuntime()), nodes, false, companionOf(t, Companion{FFmpeg: ffmpeg, FaceFusion: faceFusion, Codex: true}))
	s.Nodes = nodes
	return s
}

// open is the screen over the rig (llmPinned: the settings file of the LLM is the one a start option named); with mods changes the services of the settings routes before the router is built.
func open(t *testing.T, rig *mmtest.Rig, rts *lrtest.Rig, nodes *nodetest.Rig, llmPinned bool, mods ...func(*settingsroutes.Services)) *Screen {
	t.Helper()
	db := testdb.Open(t)
	svc := servicetest.New(t, service.Deps{DB: db, Credentials: credentialtest.New()})
	llmFile := filepath.Join(t.TempDir(), "llm.json")
	settings := settingsroutes.New(llmFile, llmPinned, filepath.Join(t.TempDir(), "m.json"), false)
	keys := credentialtest.New()
	settings.LLMLocal = llmlocal.New(llmFile, llmPinned, keys, nil)
	settings.ModelManager, settings.Gentest = rig.Svc, rig.Gentest
	// The jobs of the model manager and of the generation test are in the rig's table; the Projects and Shots in the services'.
	works := &worktrack.Tracker{}
	settings.JobList = joblist.New(joblist.Deps{
		Table: jobrecent.New(rig.DB()), Shots: repository.NewGenerationRepository(db), Projects: svc.Projects, ModelManager: rig.Svc, Gentest: rig.Gentest,
		Works: []worktrack.Source{works},
	})
	if rts != nil {
		settings.LocalRuntimes = rts.Svc
	}
	if nodes != nil {
		settings.ModelNodes = nodes.Svc
	}
	for _, mod := range mods {
		mod(settings)
	}
	s := &Screen{T: t, Rig: rig, Svc: svc, Rts: rts, Works: works, LLM: settings.LLMLocal, LLMFile: llmFile, LLMKeys: keys, engine: router.New(svc, router.Options{Settings: settings})}

	rec := s.send(httptest.NewRequest(http.MethodGet, paths.ModelManager(), nil))
	require.Equal(t, http.StatusOK, rec.Code)
	for _, c := range rec.Result().Cookies() {
		if c.Name == middleware.CSRFCookieName {
			s.cookie = c
		}
	}
	require.NotNil(t, s.cookie)
	m := regexp.MustCompile(`<meta name="csrf-token" content="([^"]+)"`).FindStringSubmatch(rec.Body.String())
	require.Len(t, m, 2)
	s.token = m[1]
	return s
}

func (s *Screen) send(req *http.Request) *httptest.ResponseRecorder {
	req.Host, req.RemoteAddr = "127.0.0.1:8787", "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	s.engine.ServeHTTP(rec, req)
	return rec
}

// Send sends the request as a client of the screen (the host and the address of a local one; no cookie or header is added).
func (s *Screen) Send(req *http.Request) *httptest.ResponseRecorder { return s.send(req) }

// Do sends an HTMX request with the form (nil: none).
func (s *Screen) Do(method, path string, form url.Values) *httptest.ResponseRecorder {
	s.T.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.AddCookie(s.cookie)
	req.Header.Set(middleware.CSRFHeaderName, s.token)
	return s.send(req)
}

// Get sends an HTMX GET.
func (s *Screen) Get(path string) *httptest.ResponseRecorder { return s.Do(http.MethodGet, path, nil) }

// Visit is a browser's GET of a whole page (no HX-Request) at path.
func (s *Screen) Visit(path string) *httptest.ResponseRecorder {
	s.T.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.AddCookie(s.cookie)
	return s.send(req)
}

// Page is GET /model-manager (the whole page: the overview).
func (s *Screen) Page() string { return s.PageAt(paths.ModelManager()) }

// PageOf is GET /model-manager/<slug> (the whole page of a category).
func (s *Screen) PageOf(slug string) string { return s.PageAt(paths.ModelManagerCategory(slug)) }

// PageAt is the whole page at path.
func (s *Screen) PageAt(path string) string {
	s.T.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.AddCookie(s.cookie)
	rec := s.send(req)
	require.Equal(s.T, http.StatusOK, rec.Code)
	return rec.Body.String()
}

// WaitJob runs the queue until the job ended.
func (s *Screen) WaitJob(id string) {
	s.T.Helper()
	require.Eventually(s.T, func() bool {
		s.Rig.Start()
		st, err := s.Rig.Svc.JobStatus(s.T.Context(), id)
		return err == nil && (st.State == domain.JobSucceeded || st.State == domain.JobFailed)
	}, 30*time.Second, 20*time.Millisecond)
}

// JobID reads the job id a polling section or a progress bar carries.
func JobID(t *testing.T, body string) string {
	t.Helper()
	m := regexp.MustCompile(`data-job="([^"]+)"`).FindStringSubmatch(body)
	require.Len(t, m, 2, body)
	return m[1]
}
