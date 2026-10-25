package web_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/router/settingsroutes"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

const savedNotice = "保存しました。反映にはアプリの再起動が必要です"

// llmScreen is the settings screen over an llm.json of its own.
type llmScreen struct {
	t      *testing.T
	file   string
	engine http.Handler
	cookie *http.Cookie
	token  string
}

func newLLMScreen(t *testing.T, pinned bool) *llmScreen {
	t.Helper()
	file := filepath.Join(t.TempDir(), "data", "llm.json")
	svc := servicetest.New(t, service.Deps{DB: testdb.Open(t), Credentials: credentialtest.New()})
	s := &llmScreen{t: t, file: file, engine: router.New(svc, router.Options{Settings: settingsroutes.New(file, pinned, filepath.Join(t.TempDir(), "models.json"), false)})}
	rec := s.send(httptest.NewRequest(http.MethodGet, paths.Settings(), nil))
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

func (s *llmScreen) send(req *http.Request) *httptest.ResponseRecorder {
	req.Host, req.RemoteAddr = "127.0.0.1:8787", "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	s.engine.ServeHTTP(rec, req)
	return rec
}

func (s *llmScreen) request(method string, form url.Values) *httptest.ResponseRecorder {
	return s.requestTo(method, paths.LLMSettings(), form)
}

// requestTo is an HTMX request with the CSRF token, as the page's script sends one.
func (s *llmScreen) requestTo(method, path string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.AddCookie(s.cookie)
	req.Header.Set(middleware.CSRFHeaderName, s.token)
	return s.send(req)
}

func (s *llmScreen) page() string {
	req := httptest.NewRequest(http.MethodGet, paths.LLMSettings(), nil)
	req.AddCookie(s.cookie)
	rec := s.send(req)
	require.Equal(s.t, http.StatusOK, rec.Code)
	return rec.Body.String()
}

// defaultForm is the form of the screen as it is first shown: every field holds the default.
func defaultForm() url.Values {
	form := url.Values{}
	for a, r := range llm.DefaultConfig().Routes {
		for role, t := range map[string]llm.Target{"primary": r.Primary, "fallback": r.Fallback} {
			form.Set(string(a)+"."+role+".provider", string(t.Provider))
			form.Set(string(a)+"."+role+".model", t.Model)
		}
	}
	return form
}

// agentForm is the form of the modal of an Agent as the browser sends it.
func agentForm(primaryProvider, primaryModel, fallbackProvider, fallbackModel string) url.Values {
	return url.Values{
		"primary.provider": {primaryProvider}, "primary.model": {primaryModel},
		"fallback.provider": {fallbackProvider}, "fallback.model": {fallbackModel},
	}
}

func inputValue(t *testing.T, html, name string) string {
	t.Helper()
	m := regexp.MustCompile(`<input[^>]*name="` + regexp.QuoteMeta(name) + `"[^>]*>`).FindString(html)
	require.NotEmpty(t, m, name)
	v := regexp.MustCompile(`value="([^"]*)"`).FindStringSubmatch(m)
	require.Len(t, v, 2, m)
	return v[1]
}
