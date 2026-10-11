package router_test

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
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
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
)

func init() { gin.SetMode(gin.TestMode) }

func newEngine(t *testing.T) *gin.Engine {
	t.Helper()
	keyring.MockInit()
	db := testdb.Open(t)
	svc := service.New(service.Deps{DB: db, Credentials: credential.NewKeyring()})
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

func TestCSRF_IgnoresTheBodyAndNeverBuffersAnUpload(t *testing.T) {
	engine := newEngine(t)
	c, token := bootstrap(t, engine)
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)

	// The token in a form field is not accepted: the body is not read at all.
	form := url.Values{"csrf_token": {token}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(c)
	assert.Equal(t, http.StatusForbidden, do(engine, req).Code)

	// A multipart upload larger than gin's default 32 MiB form memory, valid cookie, no header:
	// rejected before any parsing, so nothing is spilled to a temp file.
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", "big.bin")
	require.NoError(t, err)
	_, err = part.Write(make([]byte, 33<<20))
	require.NoError(t, err)
	require.NoError(t, w.WriteField("csrf_token", token))
	require.NoError(t, w.Close())
	req = httptest.NewRequest(http.MethodPost, "/projects", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.AddCookie(c)
	assert.Equal(t, http.StatusForbidden, do(engine, req).Code)
	entries, err := os.ReadDir(tmp)
	require.NoError(t, err)
	assert.Empty(t, entries, "no temp file for a request that failed CSRF")
}
