package themestate_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/web/themestate"
)

func chosenFor(t *testing.T, cookie *http.Cookie) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(themestate.Middleware())
	var chosen string
	engine.GET("/", func(c *gin.Context) { chosen = themestate.Chosen(c.Request.Context()) })
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	engine.ServeHTTP(httptest.NewRecorder(), req)
	return chosen
}

func TestMiddleware_ReadsOnlyTheTwoThemes(t *testing.T) {
	assert.Equal(t, themestate.Dark, chosenFor(t, &http.Cookie{Name: themestate.CookieName, Value: themestate.Dark}))
	assert.Equal(t, themestate.Light, chosenFor(t, &http.Cookie{Name: themestate.CookieName, Value: themestate.Light}))
	assert.Empty(t, chosenFor(t, &http.Cookie{Name: themestate.CookieName, Value: `"><script>`}), "an unknown value is no choice")
	assert.Empty(t, chosenFor(t, nil), "no cookie: follow the system")
}

func TestChosen_NoStateFollowsTheSystem(t *testing.T) {
	assert.Empty(t, themestate.Chosen(t.Context()))
	assert.Equal(t, themestate.Dark, themestate.Chosen(themestate.With(t.Context(), themestate.Dark)))
	assert.Empty(t, themestate.Chosen(themestate.With(t.Context(), "sepia")))
}
