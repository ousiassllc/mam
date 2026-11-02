package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/web/middleware"
)

func TestSecurityHeaders_PolicyForTheRequestHost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.SecurityHeaders())
	engine.GET("/page", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	engine.GET("/asset", func(c *gin.Context) {
		c.Header("Content-Security-Policy", "sandbox")
		c.String(http.StatusOK, "ok")
	})
	get := func(path, host string) http.Header {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = host
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		return rec.Header()
	}

	h := get("/page", "127.0.0.1:8787")
	assert.Equal(t, "nosniff", h.Get("X-Content-Type-Options"))
	csp := h.Get("Content-Security-Policy")
	for _, want := range []string{
		"default-src 'self'", "script-src 'self'", "style-src 'self' 'unsafe-inline'",
		"connect-src 'self' ws://127.0.0.1:8787;", "img-src 'self' data: blob:", "media-src 'self' blob:",
		"frame-ancestors 'none'", "base-uri 'none'", "form-action 'self'",
	} {
		assert.Contains(t, csp, want)
	}
	assert.NotContains(t, csp, "script-src 'self' 'unsafe", "no inline script or eval")

	assert.Contains(t, get("/page", "localhost:9000").Get("Content-Security-Policy"), "connect-src 'self' ws://localhost:9000;")
	assert.Contains(t, get("/page", "[::1]:8787").Get("Content-Security-Policy"), "connect-src 'self' ws://[::1]:8787;")

	// A Host that is not a plain loopback host:port never reaches the header.
	for _, host := range []string{"127.0.0.1:80;script-src *", "evil.example.com", "127.0.0.1:0x50", "127.0.0.1:99999"} {
		assert.Contains(t, get("/page", host).Get("Content-Security-Policy"), "connect-src 'self'; img-src", host)
	}

	// A handler's own policy (assets are served with `sandbox`) wins.
	h = get("/asset", "127.0.0.1:8787")
	assert.Equal(t, "sandbox", h.Get("Content-Security-Policy"))
	assert.Equal(t, "nosniff", h.Get("X-Content-Type-Options"))
}

func TestAllowFrame_AddsFrameSrcForExactlyOneOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.SecurityHeaders())
	engine.GET("/plain", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	engine.GET("/nodes", func(c *gin.Context) {
		assert.True(t, middleware.AllowFrame(c, c.Query("origin")) == (c.Query("want") == "yes"), c.Query("origin"))
		c.String(http.StatusOK, "ok")
	})
	csp := func(target string) string {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.Host = "127.0.0.1:8787"
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		return rec.Header().Get("Content-Security-Policy")
	}

	// Allowed direction: the origin is in frame-src, and frame-ancestors and the rest are untouched.
	got := csp("/nodes?want=yes&origin=http://127.0.0.1:8188")
	assert.Contains(t, got, "; frame-src http://127.0.0.1:8188; frame-ancestors 'none';")
	assert.Equal(t, 1, strings.Count(got, "frame-src"))
	assert.Contains(t, csp("/nodes?want=yes&origin=https://box.tailnet.ts.net:8188"), "frame-src https://box.tailnet.ts.net:8188;")
	assert.Contains(t, csp("/nodes?want=yes&origin=http://[::1]:8188"), "frame-src http://[::1]:8188;")

	// Not allowed direction: a page that did not ask has no frame-src (frames fall back to default-src 'self'), and
	// the origin must be a plain origin: nothing that could add a directive or a source.
	plain := csp("/plain")
	assert.NotContains(t, plain, "frame-src")
	assert.Contains(t, plain, "frame-ancestors 'none'")
	assert.Contains(t, plain, "default-src 'self'")
	for _, bad := range []string{
		"http://127.0.0.1:8188;%20script-src%20*", "http://a.example%20http://b.example", "*", "'self'", "data:", "http://127.0.0.1:0",
		"http://127.0.0.1:99999", "ftp://127.0.0.1:8188", "http://127.0.0.1:8188/path", "http://", "",
	} {
		assert.NotContains(t, csp("/nodes?want=no&origin="+bad), "frame-src", bad)
	}
	for _, c := range []string{csp("/nodes?want=yes&origin=http://127.0.0.1:8188"), plain} {
		assert.Contains(t, c, "frame-ancestors 'none'", "the app is still never framed")
	}
}

func TestAllowFrame_LeavesAHandlersOwnPolicyAlone(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.SecurityHeaders())
	engine.GET("/asset", func(c *gin.Context) {
		c.Header("Content-Security-Policy", "sandbox")
		assert.False(t, middleware.AllowFrame(c, "http://127.0.0.1:8188"))
		c.String(http.StatusOK, "ok")
	})
	req := httptest.NewRequest(http.MethodGet, "/asset", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	assert.Equal(t, "sandbox", rec.Header().Get("Content-Security-Policy"))
}
