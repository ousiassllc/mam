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
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/router/settingsroutes"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime/lrtest"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// Screen is the screen under test.
type Screen struct {
	T   *testing.T
	Rig *mmtest.Rig
	// Rts are the local runtimes (NewWithRuntimes); nil for a screen without them.
	Rts    *lrtest.Rig
	engine http.Handler
	cookie *http.Cookie
	token  string
}

// New opens the screen: it loads the page for the CSRF cookie and token.
func New(t *testing.T, opts ...mmtest.Option) *Screen {
	t.Helper()
	return open(t, mmtest.New(t, opts...), nil)
}

// NewWithRuntimes opens the screen with the local runtimes over fakes (a fake package and weights, a fake process; a Windows PC with an RTX
// 4070 unless an option says another), on the queue of the model manager.
func NewWithRuntimes(t *testing.T, opts ...lrtest.Option) *Screen {
	t.Helper()
	rig := mmtest.New(t)
	return open(t, rig, lrtest.New(t, rig.Queue, rig.Table, rig.Svc.HandleInstall, opts...))
}

func open(t *testing.T, rig *mmtest.Rig, rts *lrtest.Rig) *Screen {
	t.Helper()
	svc := servicetest.New(t, service.Deps{DB: testdb.Open(t), Credentials: credentialtest.New()})
	settings := settingsroutes.New(filepath.Join(t.TempDir(), "llm.json"), false, filepath.Join(t.TempDir(), "m.json"), false)
	settings.ModelManager, settings.Gentest = rig.Svc, rig.Gentest
	if rts != nil {
		settings.LocalRuntimes = rts.Svc
	}
	s := &Screen{T: t, Rig: rig, Rts: rts, engine: router.New(svc, router.Options{Settings: settings})}

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

// Page is GET /model-manager (the whole page).
func (s *Screen) Page() string {
	s.T.Helper()
	req := httptest.NewRequest(http.MethodGet, paths.ModelManager(), nil)
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
