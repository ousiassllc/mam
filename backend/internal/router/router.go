// Package router defines the page, action and API routes (spec/architecture/overview.md §7).
// The returned *gin.Engine is a plain http.Handler, so the run mode (browser server
// today, native shell later) does not affect anything below it.
package router

import (
	"io/fs"
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/router/settingsroutes"
	"github.com/ousiassllc/mam/backend/internal/router/voiceroutes"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/handler"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/navstate"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
	"github.com/ousiassllc/mam/backend/internal/web/themestate"
)

// Options are the non-service inputs of New.
type Options struct {
	Logger *slog.Logger
	// Static serves /static/*. Callers pass static.Dist() or os.DirFS for `make dev`.
	Static   fs.FS
	Settings *settingsroutes.Services // the LLM / model sections of the settings screen; nil leaves them out
}

// New builds the engine. Middleware order matters: recovery and logging wrap everything; the loopback guard runs before CSRF so foreign
// hosts get nothing, not even a token cookie. Routes: pages.go, actions.go (fragments), api.go (Huma JSON), settingsroutes, voiceroutes.
func New(services *service.Services, opts Options) *gin.Engine {
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	engine := gin.New()
	handler.RouteErrors(engine)
	engine.Use(
		middleware.Recovery(logger), middleware.Logger(logger),
		middleware.LoopbackOnly(), middleware.SecurityHeaders(), middleware.CSRF(), middleware.MaxBody(),
		navstate.Middleware(), themestate.Middleware(), // the side menu's folded state and the chosen colour theme, read from their cookies
	)

	registerStatic(engine, opts.Static)
	registerPages(engine, services, settingsroutes.OnboardingHome(opts.Settings, handler.Projects(services.Projects)))
	registerActions(engine, services)
	registerAPI(engine, services)
	settingsroutes.Register(engine, services, opts.Settings, logger)
	voiceroutes.Register(engine, services)
	return engine
}

func registerStatic(engine *gin.Engine, static fs.FS) {
	if static != nil {
		engine.StaticFS(paths.StaticPrefix, httpFS(static))
	}
}
