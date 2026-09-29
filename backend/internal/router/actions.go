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
	engine.PUT("/projects/:id/production-template", handler.SetProductionTemplate(services.Policy))
	engine.PUT("/projects/:id/quality-policy", handler.SetQualityPolicy(services.Policy))
	engine.PUT("/projects/:id/budget", handler.SetBudget(services.Policy))
	engine.POST("/projects/:id/takes/:takeId/archive", handler.ArchiveTake(services.Policy))
	engine.POST("/projects/:id/storage/archive-unneeded", handler.ArchiveUnneededTakes(services.Policy))

	engine.POST("/projects/:id/scenes", handler.CreateScene(services.Plans))
	engine.POST("/scenes/:id/shots", handler.CreateShot(services.Plans))

	engine.POST("/shots/:id/generate", handler.GenerateShot(services.Generation))
	engine.POST("/shots/:id/reference-pack", handler.BuildReferencePack(services.Generation))
	engine.POST("/projects/:id/messages", handler.PostMessage(services.Chat))
	engine.POST("/projects/:id/approvals/:messageId", handler.RespondApproval(services.Chat))
}
