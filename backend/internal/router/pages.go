package router

import (
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/handler"
)

// registerPages registers page routes (GET, branch on HX-Request: full page or fragment).
func registerPages(engine *gin.Engine, services *service.Services) {
	engine.GET("/", handler.Home)
	engine.GET("/healthz", handler.Health(services.Health))
}
