// Package livetest_test tests the part of the history of the generation test screen that refreshes itself (#925) over a real
// Service with fake model backends, driven as the browser's HTMX does. It has its own small harness because the directory of the
// handlers is full (linterly): the one in web/handler/gentest is not importable.
package livetest_test

import (
	"bytes"
	"html"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/router/settingsroutes"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.ReleaseMode)
	m.Run()
}

type screen struct {
	t      *testing.T
	rig    *gentesttest.Rig
	engine http.Handler
	cookie *http.Cookie
	token  string
}

func newScreen(t *testing.T, opts ...gentesttest.Option) *screen {
	t.Helper()
	rig := gentesttest.New(t, opts...)
	svc := servicetest.New(t, service.Deps{DB: rig.DB, Credentials: credentialtest.New()})
	settings := settingsroutes.New(t.TempDir()+"/llm.json", false, t.TempDir()+"/m.json", false)
	settings.Gentest = rig.Svc
	s := &screen{t: t, rig: rig, engine: router.New(svc, router.Options{Settings: settings})}
	rec := s.send(httptest.NewRequest(http.MethodGet, paths.GenerationTest(), nil))
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

func (s *screen) send(req *http.Request) *httptest.ResponseRecorder {
	req.Host, req.RemoteAddr = "127.0.0.1:8787", "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	s.engine.ServeHTTP(rec, req)
	return rec
}

// htmx sends the request with what the browser's HTMX adds.
func (s *screen) htmx(req *http.Request) *httptest.ResponseRecorder {
	req.Header.Set("HX-Request", "true")
	req.AddCookie(s.cookie)
	req.Header.Set(middleware.CSRFHeaderName, s.token)
	return s.send(req)
}

func (s *screen) do(method, path string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return s.htmx(req)
}

func (s *screen) get(path string) string { return s.do(http.MethodGet, path, nil).Body.String() }

// estimate posts the form as the browser's multipart form and returns the form the confirmation posts to run.
func (s *screen) estimate(form url.Values) url.Values {
	s.t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, vs := range form {
		for _, v := range vs {
			require.NoError(s.t, w.WriteField(k, v))
		}
	}
	require.NoError(s.t, w.Close())
	req := httptest.NewRequest(http.MethodPost, paths.GenerationTestPart("estimate"), &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := s.htmx(req)
	require.Equal(s.t, http.StatusOK, rec.Code, rec.Body.String())
	confirmed := url.Values{}
	for _, m := range regexp.MustCompile(`<input type="hidden" name="([^"]*)" value="([^"]*)"`).FindAllStringSubmatch(rec.Body.String(), -1) {
		confirmed.Add(html.UnescapeString(m[1]), html.UnescapeString(m[2]))
	}
	require.NotEmpty(s.t, confirmed.Get("estimate_id"), rec.Body.String())
	return confirmed
}

// start runs the form and returns the id of the batch it started.
func (s *screen) start(form url.Values) string {
	s.t.Helper()
	rec := s.do(http.MethodPost, paths.GenerationTestPart("run"), s.estimate(form))
	require.Equal(s.t, http.StatusAccepted, rec.Code, rec.Body.String())
	list, err := s.rig.Svc.Batches(s.t.Context(), 1)
	require.NoError(s.t, err)
	require.Len(s.t, list, 1)
	return list[0].BatchID
}

// settled polls the batch as the page does until it stops asking for itself.
func (s *screen) settled(id string) {
	s.t.Helper()
	require.Eventually(s.t, func() bool {
		return !strings.Contains(s.get(paths.GenerationTestPart("batches", id)), `hx-trigger="every 2s"`)
	}, 30*time.Second, 20*time.Millisecond, "the batch did not end")
}
