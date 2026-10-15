package web_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/testenv"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// The routes of the collections are the paths the pages link and post to (web/paths): the router
// registers exactly these, so a page cannot point at a route the router does not have (#569).
func TestCollectionRoutes_AreRegisteredUnderThePathsThePagesUse(t *testing.T) {
	testenv.Parallel(t)
	gin.SetMode(gin.TestMode)
	engine := router.New(
		servicetest.New(t, service.Deps{DB: testdb.Open(t), Credentials: credentialtest.New()}),
		router.Options{Static: fstest.MapFS{"app.js": {Data: []byte("x")}, "app.css": {Data: []byte("y")}}},
	)
	serve := func(req *http.Request) *httptest.ResponseRecorder {
		req.Host, req.RemoteAddr = "127.0.0.1:8787", "127.0.0.1:54321"
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		return rec
	}
	home := serve(httptest.NewRequest(http.MethodGet, paths.Projects(), nil))
	assert.Equal(t, http.StatusOK, home.Code)
	csrf := home.Result().Cookies()[0]
	token := strings.SplitN(strings.SplitN(home.Body.String(), `name="csrf-token" content="`, 2)[1], `"`, 2)[0]

	for _, route := range []string{paths.Home(), paths.Analytics(), paths.Reviews(), paths.StaticApp, paths.StaticCSS} {
		assert.Equal(t, http.StatusOK, serve(httptest.NewRequest(http.MethodGet, route, nil)).Code, route)
	}
	// An empty body is refused by the handler; a route that does not exist is a 404 / 405.
	for _, route := range []string{paths.Projects(), paths.ProjectImport(), paths.SeriesList(), paths.ReviewImport()} {
		req := httptest.NewRequest(http.MethodPost, route, nil)
		req.AddCookie(csrf)
		req.Header.Set(middleware.CSRFHeaderName, token)
		assert.NotContains(t, []int{http.StatusNotFound, http.StatusMethodNotAllowed}, serve(req).Code, route)
	}
}
