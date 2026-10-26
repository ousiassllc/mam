package web_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// The API keys are global (the OS key store, one set for every Project): only /settings/api-keys carries the
// panel, the overview leads to it, and the landing page and the Project settings lead to the overview.
func TestGlobalSettings_HoldsTheAPIKeyPanelAndTheOtherPagesLinkToIt(t *testing.T) {
	svc := servicetest.New(t, service.Deps{DB: testdb.Open(t), Credentials: credentialtest.New()})
	project, err := svc.Projects.Create(context.Background(), service.CreateProjectInput{Content: "動画を作りたい"})
	require.NoError(t, err)
	engine := router.New(svc, router.Options{})
	get := func(target string, htmx bool) string {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.Host, req.RemoteAddr = "127.0.0.1:8787", "127.0.0.1:54321"
		if htmx {
			req.Header.Set("HX-Request", "true")
		}
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, target)
		return rec.Body.String()
	}

	page := get(paths.SettingsCategory(paths.SettingsAPIKeys), false)
	assert.Contains(t, page, "<html")
	assert.Contains(t, page, `<h1 class="mb-1 mt-3 text-xl font-semibold">設定</h1>`)
	assert.Contains(t, page, `href="`+paths.Home()+`"`, "back to the landing page")
	assert.Contains(t, page, `id="credential-panel"`)
	assert.Contains(t, page, `hx-get="/settings/credentials/kling/edit"`, "the key is typed into a modal, not into the list")
	assert.NotContains(t, page, `type="password"`, "no password field is left out in the list")
	fragment := get(paths.SettingsCategory(paths.SettingsAPIKeys), true)
	assert.NotContains(t, fragment, "<html")
	assert.Contains(t, fragment, `id="credential-panel"`)

	overview := get(paths.Settings(), false)
	assert.NotContains(t, overview, `id="credential-panel"`, "the overview only links to the category")
	assert.Contains(t, overview, `href="`+paths.SettingsCategory(paths.SettingsAPIKeys)+`"`)

	perProject := get(paths.Project(project.ID, "settings"), false)
	assert.NotContains(t, perProject, `id="credential-panel"`)
	assert.NotContains(t, perProject, `hx-put="/credentials/`)
	assert.Contains(t, perProject, `href="`+paths.Settings()+`"`)
	assert.Contains(t, get(paths.Home(), false), `href="`+paths.Settings()+`"`)
}

// Without router.Options.Settings the screen has the overview, the API keys and the version only: the categories of the
// LLM / models are not in the navigation and have no page, and their write routes do not exist (a request that passes the CSRF check finds no route).
func TestGlobalSettings_WithoutSettingsHasNoAssignmentSections(t *testing.T) {
	svc := servicetest.New(t, service.Deps{DB: testdb.Open(t), Credentials: credentialtest.New()})
	engine := router.New(svc, router.Options{})
	send := func(req *http.Request) *httptest.ResponseRecorder {
		req.Host, req.RemoteAddr = "127.0.0.1:8787", "127.0.0.1:54321"
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		return rec
	}
	page := send(httptest.NewRequest(http.MethodGet, paths.Settings(), nil))
	require.Equal(t, http.StatusOK, page.Code)
	assert.Contains(t, page.Body.String(), `<nav aria-label="設定"`)
	assert.Contains(t, page.Body.String(), `href="`+paths.SettingsCategory(paths.SettingsAPIKeys)+`"`)
	assert.Contains(t, page.Body.String(), `href="`+paths.SettingsCategory(paths.SettingsUpdate)+`"`)
	assert.NotContains(t, page.Body.String(), paths.LLMSettings(), "no LLM category in the navigation or the overview")
	assert.NotContains(t, page.Body.String(), paths.ModelSettings())
	keys := send(httptest.NewRequest(http.MethodGet, paths.SettingsCategory(paths.SettingsAPIKeys), nil))
	require.Equal(t, http.StatusOK, keys.Code)
	assert.Contains(t, keys.Body.String(), `id="credential-panel"`)
	for _, target := range []string{paths.LLMSettings(), paths.ModelSettings(), paths.MCPSettings(), paths.LocalModels()} {
		assert.Equal(t, http.StatusNotFound, send(httptest.NewRequest(http.MethodGet, target, nil)).Code, "the page of a category without its service: "+target)
	}

	var cookie *http.Cookie
	for _, c := range page.Result().Cookies() {
		if c.Name == middleware.CSRFCookieName {
			cookie = c
		}
	}
	require.NotNil(t, cookie)
	token := regexp.MustCompile(`<meta name="csrf-token" content="([^"]+)"`).FindStringSubmatch(page.Body.String())
	require.Len(t, token, 2)
	for _, target := range []string{paths.LLMSettings(), paths.ModelSettings()} {
		for _, method := range []string{http.MethodPut, http.MethodDelete} {
			req := httptest.NewRequest(method, target, strings.NewReader(""))
			req.AddCookie(cookie)
			req.Header.Set(middleware.CSRFHeaderName, token[1])
			assert.Contains(t, []int{http.StatusNotFound, http.StatusMethodNotAllowed}, send(req).Code, method+" "+target)
		}
	}
}
