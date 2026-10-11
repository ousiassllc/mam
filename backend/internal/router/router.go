// Package router defines the page, action and API routes (spec/architecture/overview.md §7).
// The returned *gin.Engine is a plain http.Handler, so the run mode (browser server
// today, native shell later) does not affect anything below it.
package router

import (
	"io/fs"
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
)

// Options are the non-service inputs of New.
type Options struct {
	Logger *slog.Logger
	// Static serves /static/*. Callers pass static.Dist() or os.DirFS for `make dev`.
	Static fs.FS
}

// New builds the engine. Middleware order matters: recovery and logging wrap
// everything; the loopback guard runs before CSRF so foreign hosts get nothing,
// not even a token cookie.
//
// To add routes, register them in the matching file (pages.go for HX-Request-aware
// page routes, actions.go for fragment-returning actions, api.go for Huma JSON
// routes); no other file needs to change.
func New(services *service.Services, opts Options) *gin.Engine {
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	engine := gin.New()
	engine.HandleMethodNotAllowed = true
	engine.Use(
		middleware.Recovery(logger),
		middleware.Logger(logger),
		middleware.LoopbackOnly(),
		middleware.SecurityHeaders(),
		middleware.CSRF(),
		middleware.MaxBody(),
	)

	registerStatic(engine, opts.Static)
	registerPages(engine, services)
	registerActions(engine, services)
	registerAPI(engine, services)
	return engine
}

func registerStatic(engine *gin.Engine, static fs.FS) {
	if static == nil {
		return
	}
	engine.StaticFS("/static", httpFS(static))
}
