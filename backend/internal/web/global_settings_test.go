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

// The API keys are global (the OS key store, one set for every Project): only /settings carries the
// panel, and the landing page and the Project settings lead to it.
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

	page := get(paths.Settings(), false)
	assert.Contains(t, page, "<html")
	assert.Contains(t, page, `<h1 class="mb-1 mt-3 text-xl font-semibold">設定</h1>`)
	assert.Contains(t, page, `href="`+paths.Home()+`"`, "back to the landing page")
	assert.Contains(t, page, `id="credential-panel"`)
	assert.Contains(t, page, `hx-put="/credentials/kling"`)
	fragment := get(paths.Settings(), true)
	assert.NotContains(t, fragment, "<html")
	assert.Contains(t, fragment, `id="credential-panel"`)

	perProject := get(paths.Project(project.ID, "settings"), false)
	assert.NotContains(t, perProject, `id="credential-panel"`)
	assert.NotContains(t, perProject, `hx-put="/credentials/`)
	assert.Contains(t, perProject, `href="`+paths.Settings()+`"`)
	assert.Contains(t, get(paths.Home(), false), `href="`+paths.Settings()+`"`)
}

// Without router.Options.Settings the screen holds the API key panel only, and the routes of the LLM / model
// sections do not exist (a request that passes the CSRF check finds no route).
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
	assert.Contains(t, page.Body.String(), `id="credential-panel"`)
	assert.NotContains(t, page.Body.String(), `id="llm-assignment-panel"`)
	assert.NotContains(t, page.Body.String(), paths.LLMSettings())
	assert.NotContains(t, page.Body.String(), paths.ModelSettings())

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
