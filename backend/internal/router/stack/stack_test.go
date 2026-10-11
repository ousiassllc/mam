// Package stack_test checks the HTTP stack as router.New assembles it, without any Project data:
// the loopback guard, health, OpenAPI, static files and the security headers.
package stack_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"

	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
)

func init() { gin.SetMode(gin.TestMode) }

func newEngine(t *testing.T) *gin.Engine {
	t.Helper()
	keyring.MockInit()
	db := testdb.Open(t)
	svc := servicetest.New(t, service.Deps{DB: db, Credentials: credential.NewKeyring()})
	return router.New(svc, router.Options{
		Static: fstest.MapFS{
			"app.js":           {Data: []byte("console.log(1)")},
			".secret":          {Data: []byte("x")},
			".hidden/file.txt": {Data: []byte("z")},
			"sub/.git/config":  {Data: []byte("z")},
			"sub/inner.js":     {Data: []byte("y")},
		},
	})
}

func do(engine *gin.Engine, req *http.Request) *httptest.ResponseRecorder {
	if req.Host == "" || req.Host == "example.com" {
		req.Host = "127.0.0.1:8787"
	}
	req.RemoteAddr = "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func TestLoopbackOnly_RejectsForeignHostOriginAndClient(t *testing.T) {
	engine := newEngine(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "evil.example.com:8787"
	assert.Equal(t, http.StatusForbidden, do(engine, req).Code, "DNS rebinding: foreign Host header")

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	assert.Equal(t, http.StatusForbidden, do(engine, req).Code, "foreign Origin")

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	req.Host = "127.0.0.1:8787"
	req.RemoteAddr = "192.168.1.20:5555"
	engine.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code, "non-loopback client")
	assert.Empty(t, rec.Result().Cookies(), "rejected requests get no token")

	for _, host := range []string{"127.0.0.1:8787", "localhost:8787", "[::1]:8787"} {
		req = httptest.NewRequest(http.MethodGet, "/", nil)
		req.Host = host
		req.Header.Set("Origin", "http://"+host)
		assert.Equal(t, http.StatusOK, do(engine, req).Code, host)
	}
}

func TestHealth_PageRouteAndHumaAPIRoute(t *testing.T) {
	engine := newEngine(t)

	rec := do(engine, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"ok":true,"database":"ok"}`, rec.Body.String())

	rec = do(engine, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"ok":true,"database":"ok"}`, rec.Body.String())
}

func TestHealth_DatabaseFailureIs503WithOKFalseOnBothRoutes(t *testing.T) {
	keyring.MockInit()
	db := testdb.Open(t)
	engine := router.New(servicetest.New(t, service.Deps{DB: db, Credentials: credential.NewKeyring()}), router.Options{})
	require.NoError(t, db.Close()) // Ping now fails

	for _, path := range []string{"/healthz", "/api/v1/health"} {
		rec := do(engine, httptest.NewRequest(http.MethodGet, path, nil))
		assert.Equal(t, http.StatusServiceUnavailable, rec.Code, path)
		assert.JSONEq(t, `{"ok":false,"database":"unavailable"}`, rec.Body.String(), path)
	}
}

func TestAPI_ServesOpenAPIButNoExternalDocsPage(t *testing.T) {
	engine := newEngine(t)

	rec := do(engine, httptest.NewRequest(http.MethodGet, "/api/v1/openapi.json", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"openapi":"3.1`)

	assert.Equal(t, http.StatusNotFound, do(engine, httptest.NewRequest(http.MethodGet, "/docs", nil)).Code)
	assert.Equal(t, http.StatusNotFound, do(engine, httptest.NewRequest(http.MethodGet, "/api/v1/docs", nil)).Code)
}

func TestStatic_ServesFilesButNotDotfilesOrListings(t *testing.T) {
	engine := newEngine(t)

	rec := do(engine, httptest.NewRequest(http.MethodGet, "/static/app.js", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "console.log(1)", rec.Body.String())

	assert.Equal(t, http.StatusNotFound, do(engine, httptest.NewRequest(http.MethodGet, "/static/.secret", nil)).Code)
	assert.Equal(t, http.StatusNotFound, do(engine, httptest.NewRequest(http.MethodGet, "/static/sub/", nil)).Code)
	for _, hidden := range []string{"/static/.hidden/file.txt", "/static/sub/.git/config", "/static/sub/../.hidden/file.txt"} {
		assert.Equal(t, http.StatusNotFound, do(engine, httptest.NewRequest(http.MethodGet, hidden, nil)).Code, hidden)
	}
	assert.Equal(t, http.StatusOK, do(engine, httptest.NewRequest(http.MethodGet, "/static/sub/inner.js", nil)).Code)
}

func TestSecurityHeaders_PagesFragmentsAndErrorsCarryTheContentSecurityPolicy(t *testing.T) {
	engine := newEngine(t)
	fragment := httptest.NewRequest(http.MethodGet, "/", nil)
	fragment.Header.Set("HX-Request", "true")

	for name, rec := range map[string]*httptest.ResponseRecorder{
		"full page":     do(engine, httptest.NewRequest(http.MethodGet, "/", nil)),
		"HTMX fragment": do(engine, fragment),
		"not found":     do(engine, httptest.NewRequest(http.MethodGet, "/nope", nil)),
	} {
		csp := rec.Header().Get("Content-Security-Policy")
		assert.Contains(t, csp, "script-src 'self';", name)
		assert.Contains(t, csp, "frame-ancestors 'none'", name)
		assert.Contains(t, csp, "connect-src 'self' ws://127.0.0.1:8787;", name, "the /ws/ WebSocket stays allowed")
		assert.NotContains(t, csp, "unsafe-eval", name)
		assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"), name)
	}
}
