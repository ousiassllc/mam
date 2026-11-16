// Package shotaudioroutes registers the routes of the audio of a Shot in the Shot Inspector (Issue #1153; spec/api/endpoints.md §2.2). They live outside router so that
// router stays inside its line limit; router.New calls Register.
package shotaudioroutes

import (
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/handler/shotaudiohandler"
)

// Register adds GET /shots/:id/audio (the section of the Inspector) and PUT /shots/:id/audio (tie or take off; behind the CSRF middleware like every action).
func Register(engine *gin.Engine, svc *service.Services) {
	engine.GET("/shots/:id/audio", shotaudiohandler.Get(svc))
	engine.PUT("/shots/:id/audio", shotaudiohandler.Put(svc))
}
