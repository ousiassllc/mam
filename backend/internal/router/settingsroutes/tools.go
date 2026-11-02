package settingsroutes

import (
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/web/handler/toolstatus"
)

// registerTools adds the status route of the model manager card of the settings screen (spec/api/endpoints-home.md).
// The screen behind the card comes from s (the service that gives it its routes), so a card without its screen has no
// status.
func registerTools(engine *gin.Engine, s *Services, logger *slog.Logger) {
	t := toolstatus.Services{}
	if s != nil {
		t.ModelManager = s.ModelManager
	}
	toolstatus.Register(engine, toolstatus.Handlers{Sources: t.Sources(), Logger: logger})
}
