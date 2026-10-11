package middleware_test

import (
	"net/http"
	"net/http/httptest"
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
		"connect-src 'self' ws://127.0.0.1:8787;", "img-src 'self' data:", "media-src 'self' blob:",
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
