package settingsroutes

import (
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/handler/toolstatus"
)

// registerTools adds the status routes of the tool cards of the home page (spec/api/endpoints-home.md). The screens
// behind the cards come from s (the services that give them their routes), so a card without its screen has no status.
func registerTools(engine *gin.Engine, services *service.Services, s *Services) {
	t := toolstatus.Services{Review: services.Review, Credentials: services.Credentials}
	if s != nil {
		t.ModelManager, t.Workflows, t.ChatGPT = s.ModelManager, s.Workflows, s.ChatGPT
	}
	toolstatus.Register(engine, toolstatus.Handlers{Sources: t.Sources()})
}
