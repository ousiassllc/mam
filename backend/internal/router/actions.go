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
	engine.PUT("/projects/:id/production-template", handler.SetProductionTemplate(services.Policy))
	engine.PUT("/projects/:id/quality-policy", handler.SetQualityPolicy(services.Policy))
	engine.PUT("/projects/:id/budget", handler.SetBudget(services.Policy))
	engine.POST("/projects/:id/takes/:takeId/archive", handler.ArchiveTake(services.Policy))
	engine.POST("/projects/:id/storage/archive-unneeded", handler.ArchiveUnneededTakes(services.Policy))

	engine.POST("/projects/:id/scenes", handler.CreateScene(services.Plans))
	engine.POST("/scenes/:id/shots", handler.CreateShot(services.Plans))

	engine.PUT("/projects/:id/characters/:characterId", handler.UpdateCharacter(services.World))
	engine.PUT("/projects/:id/products/:productId", handler.UpdateProduct(services.World))
	engine.PUT("/projects/:id/locations/:locationId", handler.UpdateLocation(services.World))
	engine.PUT("/projects/:id/world-state/locks", handler.SetWorldLock(services.World))
	engine.PUT("/projects/:id/characters/:characterId/reference", handler.AssignCharacterReference(services.World))
	engine.PUT("/projects/:id/products/:productId/reference", handler.AssignProductReference(services.World))
	engine.PUT("/projects/:id/locations/:locationId/reference", handler.AssignLocationReference(services.World))
	engine.POST("/projects/:id/references", handler.UploadReference(services.Reference))
	engine.DELETE("/projects/:id/references/:referenceId", handler.DeleteReference(services.Reference))
	engine.POST("/shots/:id/references", handler.PinReference(services.Reference))

	engine.POST("/shots/:id/generate", handler.GenerateShot(services.Generation))
	engine.POST("/shots/:id/generate/candidates", handler.GenerateCandidates(services.Generation))
	engine.POST("/shots/:id/continue", handler.ProposeContinue(services.Continue))
	engine.POST("/shots/:id/continue/confirm", handler.ConfirmContinue(services.Continue))
	engine.POST("/shots/:id/reference-pack", handler.BuildReferencePack(services.Generation))
	engine.POST("/takes/:id/judge", handler.JudgeTake(services.Judge))
	engine.POST("/takes/:id/repair", handler.RepairTake(services.Repair))
	engine.POST("/projects/:id/messages", handler.PostMessage(services.Chat))
	engine.POST("/projects/:id/approvals/:messageId", handler.RespondApproval(services.Chat))
}
