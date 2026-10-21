package web_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
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
	req := httptest.NewRequest(method, paths.LLMSettings(), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.AddCookie(s.cookie)
	req.Header.Set(middleware.CSRFHeaderName, s.token)
	return s.send(req)
}

func (s *llmScreen) page() string {
	req := httptest.NewRequest(http.MethodGet, paths.Settings(), nil)
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

func inputValue(t *testing.T, html, name string) string {
	t.Helper()
	m := regexp.MustCompile(`<input[^>]*name="` + regexp.QuoteMeta(name) + `"[^>]*>`).FindString(html)
	require.NotEmpty(t, m, name)
	v := regexp.MustCompile(`value="([^"]*)"`).FindStringSubmatch(m)
	require.Len(t, v, 2, m)
	return v[1]
}

func TestLLMSettings_TheScreenListsEveryAgentAndKeepsTheUnusedOnesApart(t *testing.T) {
	s := newLLMScreen(t, false)
	html := s.page()

	assert.Contains(t, html, `id="llm-assignment-panel"`)
	assert.Contains(t, html, `hx-put="`+paths.LLMSettings()+`"`)
	assert.Contains(t, html, `hx-delete="`+paths.LLMSettings()+`"`)
	for _, a := range llm.Agents() {
		for _, role := range []string{"primary", "fallback"} {
			assert.Contains(t, html, `name="`+string(a)+"."+role+`.provider"`, a)
			assert.Contains(t, html, `name="`+string(a)+"."+role+`.model"`, a)
		}
	}
	assert.Equal(t, llm.DefaultConfig().Routes[llm.AgentDirector].Primary.Model, inputValue(t, html, "director.primary.model"))

	// The two Agents no code calls are in a group that is closed until opened.
	unused := regexp.MustCompile(`(?s)<details[^>]*data-testid="llm-unused"[^>]*>.*?</details>`).FindString(html)
	require.NotEmpty(t, unused)
	assert.NotRegexp(t, `^<details[^>]*\sopen[\s>=]`, unused)
	assert.Contains(t, unused, "現在は使われません")
	assert.Contains(t, unused, `data-agent="orchestrator"`)
	assert.Contains(t, unused, `data-agent="final_qc"`)
	assert.NotContains(t, unused, `data-agent="director"`)
	assert.Equal(t, len(llm.Agents()), strings.Count(html, `data-testid="llm-route"`))
}

// Saving writes the file the next start loads: llm.LoadConfig on it gives the saved assignment, and the
// screen shows the saved one with the notice that a restart is needed.
func TestLLMSettings_SavedAssignmentIsWhatTheNextStartLoads(t *testing.T) {
	s := newLLMScreen(t, false)
	form := defaultForm()
	form.Set("director.primary.provider", "google")
	form.Set("director.primary.model", " gemini-3.1-pro ")
	form.Set("orchestrator.fallback.model", "claude-sonnet-x")

	rec := s.request(http.MethodPut, form)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), savedNotice)
	assert.NotContains(t, rec.Body.String(), "<html", "the answer is the section, not a page")
	assert.Equal(t, "gemini-3.1-pro", inputValue(t, rec.Body.String(), "director.primary.model"))

	cfg, err := llm.LoadConfig(s.file)
	require.NoError(t, err)
	director, err := cfg.RouteFor(llm.AgentDirector)
	require.NoError(t, err)
	assert.Equal(t, llm.Target{Provider: llm.ProviderGoogle, Model: "gemini-3.1-pro"}, director.Primary)
	orchestrator, err := cfg.RouteFor(llm.AgentOrchestrator)
	require.NoError(t, err)
	assert.Equal(t, "claude-sonnet-x", orchestrator.Fallback.Model, "the unused Agents are saved too")
	writer, err := cfg.RouteFor(llm.AgentWriter)
	require.NoError(t, err)
	assert.Equal(t, llm.DefaultConfig().Routes[llm.AgentWriter], writer)

	assert.Equal(t, "gemini-3.1-pro", inputValue(t, s.page(), "director.primary.model"), "a reload shows the saved one")
}

func TestLLMSettings_ResetBringsBackTheDefaultsAndRemovesTheFile(t *testing.T) {
	s := newLLMScreen(t, false)
	form := defaultForm()
	form.Set("writer.primary.model", "gpt-custom")
	require.Equal(t, http.StatusOK, s.request(http.MethodPut, form).Code)
	require.FileExists(t, s.file)

	rec := s.request(http.MethodDelete, nil)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "反映にはアプリの再起動が必要です")
	assert.NoFileExists(t, s.file)
	assert.Equal(t, llm.DefaultConfig().Routes[llm.AgentWriter].Primary.Model, inputValue(t, rec.Body.String(), "writer.primary.model"))
}

// A refused save answers 422 with the reason, keeps what was typed so that one mistake does not lose the
// rest, and writes nothing.
func TestLLMSettings_AnInvalidAssignmentIsRefusedAndNothingIsWritten(t *testing.T) {
	s := newLLMScreen(t, false)
	for name, mutate := range map[string]func(url.Values){
		"unknown provider": func(f url.Values) { f.Set("director.primary.provider", "nobody") },
		"empty model":      func(f url.Values) { f.Set("director.primary.model", "  ") },
		"missing fields":   func(f url.Values) { f.Del("director.fallback.provider") },
	} {
		form := defaultForm()
		form.Set("writer.primary.model", "gpt-typed")
		mutate(form)

		rec := s.request(http.MethodPut, form)

		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, name)
		assert.Contains(t, rec.Body.String(), `data-testid="llm-error"`, name)
		assert.Contains(t, rec.Body.String(), "Director", name, "the reason names the Agent")
		assert.Equal(t, "gpt-typed", inputValue(t, rec.Body.String(), "writer.primary.model"), name)
		assert.NoFileExists(t, s.file, name)
	}
}

// An operator who passed --llm-config owns the file: the screen shows it and writes nothing.
func TestLLMSettings_APinnedFileIsShownReadOnlyAndNeverWritten(t *testing.T) {
	s := newLLMScreen(t, true)
	html := s.page()

	assert.Contains(t, html, "起動オプション（--llm-config / MAM_LLM_CONFIG）で固定されています")
	assert.NotContains(t, html, `hx-put="`+paths.LLMSettings()+`"`)
	assert.NotContains(t, html, `hx-delete="`+paths.LLMSettings()+`"`)
	assert.Contains(t, html, `data-testid="llm-readonly"`)

	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		rec := s.request(method, defaultForm())
		assert.Equal(t, http.StatusConflict, rec.Code, method)
		assert.Contains(t, rec.Body.String(), "固定されています", method)
	}
	assert.NoFileExists(t, s.file)
}
