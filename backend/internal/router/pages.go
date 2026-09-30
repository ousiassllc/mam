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
	engine.GET("/shots/:id", handler.ShotInspector(services.Director))
	engine.GET("/shots/:id/takes", handler.TakeComparison(services.Takes))
	engine.GET("/takes/:id/video", handler.TakeVideo(services.Takes))
	engine.GET("/takes/:id/thumbnail", handler.TakeThumbnail(services.Takes))
	engine.GET("/projects/:id/world-state", handler.WorldStatePage(services.World))
	engine.GET("/projects/:id/references", handler.ReferenceLibraryPage(services.Reference))
	engine.GET("/series/:id", handler.SeriesPage(services.Series))
	engine.GET("/projects/:id/assets/:assetId", handler.ProjectAsset(services.Projects))
	engine.GET("/projects/:id/settings", handler.SettingsPage(services.Policy))
	engine.GET("/projects/:id/cost-summary", handler.CostSummary(services.Policy))
	engine.GET("/projects/:id/bundle", handler.ExportBundle(services.Bundle))
	engine.GET("/takes/:id/trace", handler.TakeTrace(services.Trace))
	engine.GET("/branches/:id/versions", handler.BranchVersions(services.Versions))
	engine.GET("/projects/:id/edit", handler.EditorPage(services.Editor))
	engine.GET("/projects/:id/export", handler.EditorPage(services.Editor))
	engine.GET("/projects/:id/edit/actions", handler.EditorActions(services.Editor))
	engine.GET("/edit-timelines/:id/preview", handler.EditPreview(services.Editor))
	engine.GET("/projects/:id/exports/:exportId/video", handler.ExportVideo(services.Editor))

	// WebSocket: notifications only, no HTML (spec/architecture/overview.md §7).
	engine.GET("/ws/projects/:id", handler.ProjectEvents(services.Chat))
	engine.GET("/ws/shots/:id", handler.ShotProgress(services.Generation))
}
