package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/web/middleware"
)

func TestLoopbackOnly_StateChangingRequestsNeedTheOriginOfThisServer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.LoopbackOnly())
	engine.Any("/x", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	send := func(method, host, origin, upgrade string) int {
		req := httptest.NewRequest(method, "/x", nil)
		req.RemoteAddr = "127.0.0.1:50000"
		req.Host = host
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if upgrade != "" {
			req.Header.Set("Upgrade", upgrade)
		}
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		return rec.Code
	}

	assert.Equal(t, http.StatusForbidden, send(http.MethodPost, "127.0.0.1:8787", "http://127.0.0.1:9999", ""), "another port of the same host")
	assert.Equal(t, http.StatusForbidden, send(http.MethodPost, "127.0.0.1:8787", "http://localhost:8787", ""), "another loopback name")
	assert.Equal(t, http.StatusForbidden, send(http.MethodDelete, "localhost:8787", "http://localhost:9999", ""))
	assert.Equal(t, http.StatusForbidden, send(http.MethodGet, "127.0.0.1:8787", "http://127.0.0.1:9999", "websocket"), "WebSocket handshake")
	assert.Equal(t, http.StatusForbidden, send(http.MethodPost, "127.0.0.1:8787", "https://evil.example.com", ""), "foreign host")

	assert.Equal(t, http.StatusNoContent, send(http.MethodPost, "127.0.0.1:8787", "http://127.0.0.1:8787", ""), "same origin")
	assert.Equal(t, http.StatusNoContent, send(http.MethodPost, "localhost:8787", "http://LOCALHOST:8787", ""))
	assert.Equal(t, http.StatusNoContent, send(http.MethodGet, "127.0.0.1:8787", "http://127.0.0.1:8787", "websocket"))
	assert.Equal(t, http.StatusNoContent, send(http.MethodPost, "127.0.0.1:8787", "", ""), "no Origin header")
	assert.Equal(t, http.StatusNoContent, send(http.MethodGet, "127.0.0.1:8787", "http://127.0.0.1:9999", ""), "a plain read is not restricted by this rule")
}
