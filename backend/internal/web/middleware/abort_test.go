package middleware_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/web/middleware"
)

func newEngine(mw ...gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(mw...)
	engine.POST("/x", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	engine.POST("/panic", func(*gin.Context) { panic("boom") })
	return engine
}

func post(engine *gin.Engine, path string, hx bool, edit func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("a=b"))
	req.RemoteAddr = "127.0.0.1:50000"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if hx {
		req.Header.Set("HX-Request", "true")
	}
	if edit != nil {
		edit(req)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// htmx swaps only text/html error responses: a middleware failure of an HX-Request must be a toast.
func assertToast(t *testing.T, rec *httptest.ResponseRecorder, status int) {
	t.Helper()
	assert.Equal(t, status, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/html")
	assert.Equal(t, "none", rec.Header().Get("HX-Reswap"))
	assert.Equal(t, "false", rec.Header().Get("HX-Push-Url"), "a failed request must not push its URL into the history")
	assert.Contains(t, rec.Body.String(), `id="toast-region"`)
	assert.Contains(t, rec.Body.String(), "data-toast-message")
}

func TestCSRF_RejectsAnHTMXRequestWithAToastAndOthersWithAProblem(t *testing.T) {
	engine := newEngine(middleware.CSRF())

	assertToast(t, post(engine, "/x", true, nil), http.StatusForbidden)

	rec := post(engine, "/x", false, nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"status":403,"title":"invalid or missing CSRF token","detail":"送信を確認できませんでした。画面を再読み込みしてからやり直してください"}`, rec.Body.String())
}

func TestMaxBody_RefusesAnOversizedHTMXRequestWithAToast(t *testing.T) {
	engine := newEngine(middleware.MaxBody())
	big := func(r *http.Request) { r.ContentLength = middleware.MaxBodyBytes + 1 }

	assertToast(t, post(engine, "/x", true, big), http.StatusRequestEntityTooLarge)

	rec := post(engine, "/x", false, big)
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	assert.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"status":413,"title":"送信内容が大きすぎます","detail":"送信内容が大きすぎます"}`, rec.Body.String())
}

func TestLoopbackOnly_RefusesAForeignHTMXRequestWithAToast(t *testing.T) {
	engine := newEngine(middleware.LoopbackOnly())
	foreign := func(r *http.Request) { r.Host = "evil.example.com" }

	assertToast(t, post(engine, "/x", true, foreign), http.StatusForbidden)

	rec := post(engine, "/x", false, foreign)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Empty(t, rec.Body.String())
}

func TestRecovery_AnswersAPanicOfAnHTMXRequestWithAToast(t *testing.T) {
	engine := newEngine(middleware.Recovery(slog.New(slog.NewTextHandler(io.Discard, nil))))

	assertToast(t, post(engine, "/panic", true, nil), http.StatusInternalServerError)

	rec := post(engine, "/panic", false, nil)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Empty(t, rec.Body.String())
}
