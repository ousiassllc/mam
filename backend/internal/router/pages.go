package router

import (
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/handler"
)

// registerPages registers page routes (GET, branch on HX-Request: full page or fragment).
func registerPages(engine *gin.Engine, services *service.Services) {
	engine.GET("/", handler.Projects(services.Projects))
	engine.GET("/healthz", handler.Health(services.Health))

	engine.GET("/projects", handler.Projects(services.Projects))
	engine.GET("/projects/:id", handler.ChatWorkspace(services.Chat))
	engine.GET("/projects/:id/messages", handler.ChatMessages(services.Chat))
	engine.GET("/projects/:id/messages/:messageId", handler.ChatCard(services.Chat))
	engine.GET("/projects/:id/brief", handler.BriefPage(services.Projects))
	engine.GET("/projects/:id/scenes", handler.ScenePage(services.Plans))
	engine.GET("/projects/:id/scenes/:sceneId", handler.ScenePage(services.Plans))
	engine.GET("/projects/:id/assets/:assetId", handler.ProjectAsset(services.Projects))

	// WebSocket: notifications only, no HTML (spec/architecture/overview.md §7).
	engine.GET("/ws/projects/:id", handler.ProjectEvents(services.Chat))
}
