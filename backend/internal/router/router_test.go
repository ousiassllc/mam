package router_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"

	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
)

func init() { gin.SetMode(gin.TestMode) }

func newEngine(t *testing.T) *gin.Engine {
	t.Helper()
	keyring.MockInit()
	db, err := repository.Open(context.Background(), filepath.Join(t.TempDir(), "mam.db"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	svc := service.New(service.Deps{DB: db, Credentials: credential.NewKeyring()})
	return router.New(svc, router.Options{
		Static: fstest.MapFS{
			"app.js":       {Data: []byte("console.log(1)")},
			".secret":      {Data: []byte("x")},
			"sub/inner.js": {Data: []byte("y")},
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

func cookie(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

var metaRe = regexp.MustCompile(`<meta name="csrf-token" content="([^"]+)"`)

// bootstrap does what a browser does: GET a page, keep the cookie, read the meta token.
func bootstrap(t *testing.T, engine *gin.Engine) (*http.Cookie, string) {
	t.Helper()
	rec := do(engine, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	c := cookie(rec, middleware.CSRFCookieName)
	require.NotNil(t, c)
	m := metaRe.FindStringSubmatch(rec.Body.String())
	require.Len(t, m, 2, "page must publish the CSRF token in a meta tag")
	return c, m[1]
}

func TestHome_RendersPageWithCSRFMetaMatchingCookie(t *testing.T) {
	engine := newEngine(t)
	c, token := bootstrap(t, engine)
	assert.Equal(t, c.Value, token)
	assert.True(t, c.HttpOnly)
	assert.Equal(t, http.SameSiteStrictMode, c.SameSite)
}

func TestHome_ReusesExistingCSRFCookie(t *testing.T) {
	engine := newEngine(t)
	c, token := bootstrap(t, engine)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(c)
	rec := do(engine, req)
	assert.Contains(t, rec.Body.String(), `content="`+token+`"`)
	assert.Nil(t, cookie(rec, middleware.CSRFCookieName), "a valid cookie is not rotated")
}

func TestCSRF_RejectsUnsafeRequestsWithoutValidToken(t *testing.T) {
	engine := newEngine(t)
	c, token := bootstrap(t, engine)

	post := func(mutate func(*http.Request)) int {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/health", nil)
		mutate(req)
		return do(engine, req).Code
	}

	assert.Equal(t, http.StatusForbidden, post(func(*http.Request) {}), "no cookie, no token")
	assert.Equal(t, http.StatusForbidden, post(func(r *http.Request) { r.AddCookie(c) }), "cookie only")
	assert.Equal(t, http.StatusForbidden, post(func(r *http.Request) {
		r.AddCookie(c)
		r.Header.Set(middleware.CSRFHeaderName, "wrong")
	}), "wrong token")
	assert.Equal(t, http.StatusForbidden, post(func(r *http.Request) {
		r.Header.Set(middleware.CSRFHeaderName, token)
	}), "token without the cookie")

	// A valid token passes CSRF; the route itself only allows GET, hence 405 rather than 403.
	assert.Equal(t, http.StatusMethodNotAllowed, post(func(r *http.Request) {
		r.AddCookie(c)
		r.Header.Set(middleware.CSRFHeaderName, token)
	}))
}

func TestCSRF_AcceptsFormFieldForPlainForms(t *testing.T) {
	engine := newEngine(t)
	c, token := bootstrap(t, engine)

	form := url.Values{middleware.CSRFFormField: {token}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(c)
	assert.Equal(t, http.StatusMethodNotAllowed, do(engine, req).Code, "passes CSRF; POST / is not routed")
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
	assert.Nil(t, cookie(rec, middleware.CSRFCookieName), "rejected requests get no token")

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
}
