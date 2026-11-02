package navstate_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/web/navstate"
)

func foldedFor(t *testing.T, cookie *http.Cookie) bool {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(navstate.Middleware())
	var folded bool
	engine.GET("/", func(c *gin.Context) { folded = navstate.IsFolded(c.Request.Context()) })
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	engine.ServeHTTP(httptest.NewRecorder(), req)
	return folded
}

func TestMiddleware_FoldedOnlyWhenTheCookieSaysFolded(t *testing.T) {
	assert.True(t, foldedFor(t, &http.Cookie{Name: navstate.CookieName, Value: navstate.Folded}))
	assert.False(t, foldedFor(t, &http.Cookie{Name: navstate.CookieName, Value: "open"}), "open is not folded")
	assert.False(t, foldedFor(t, &http.Cookie{Name: navstate.CookieName, Value: "anything"}), "an unknown value is read as open")
	assert.False(t, foldedFor(t, nil), "no cookie: open")
}

func TestIsFolded_NoStateIsOpen(t *testing.T) {
	assert.False(t, navstate.IsFolded(t.Context()))
	assert.True(t, navstate.IsFolded(navstate.With(t.Context(), true)))
}
