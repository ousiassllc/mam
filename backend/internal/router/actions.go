package router

import (
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/handler"
)

// registerActions registers action routes (POST/PUT/DELETE, never branch on
// HX-Request, return HTML fragments only). All of them are behind the CSRF middleware.
func registerActions(engine *gin.Engine, services *service.Services) {
	engine.POST("/projects", handler.CreateProject(services.Projects))
	engine.PUT("/projects/:id/brief", handler.UpdateBrief(services.Projects))
	engine.PUT("/projects/:id/confirmation-level", handler.SetConfirmationLevel(services.Projects))
	engine.PUT("/projects/:id/mode", handler.SetMode(services.Director))
	engine.PUT("/projects/:id/locks", handler.SetProjectLock(services.Director))
	engine.PUT("/shots/:id/locks", handler.SetShotLock(services.Director))
	engine.PUT("/shots/:id/shot-spec", handler.EditShotSpec(services.Director))
	engine.POST("/shots/:id/director-command", handler.DirectorCommand(services.Director))

	engine.POST("/projects/:id/scenes", handler.CreateScene(services.Plans))
	engine.POST("/scenes/:id/shots", handler.CreateShot(services.Plans))

	engine.PUT("/projects/:id/characters/:characterId", handler.UpdateCharacter(services.World))
	engine.PUT("/projects/:id/products/:productId", handler.UpdateProduct(services.World))
	engine.PUT("/projects/:id/locations/:locationId", handler.UpdateLocation(services.World))

	engine.POST("/shots/:id/generate", handler.GenerateShot(services.Generation))
	engine.POST("/shots/:id/reference-pack", handler.BuildReferencePack(services.Generation))
	engine.POST("/projects/:id/messages", handler.PostMessage(services.Chat))
	engine.POST("/projects/:id/approvals/:messageId", handler.RespondApproval(services.Chat))
}
