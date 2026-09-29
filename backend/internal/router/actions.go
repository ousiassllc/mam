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

	engine.POST("/projects/:id/scenes", handler.CreateScene(services.Plans))
	engine.POST("/scenes/:id/shots", handler.CreateShot(services.Plans))

	engine.POST("/shots/:id/generate", handler.GenerateShot(services.Generation))
	engine.POST("/shots/:id/reference-pack", handler.BuildReferencePack(services.Generation))
	engine.POST("/takes/:id/judge", handler.JudgeTake(services.Judge))
	engine.POST("/projects/:id/messages", handler.PostMessage(services.Chat))
	engine.POST("/projects/:id/approvals/:messageId", handler.RespondApproval(services.Chat))
}
