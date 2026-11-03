// Package characterroutes registers the routes that make a Character by hand (#1001): in the World State of a Project and in the canon of
// a Series. It lives outside router so that router stays inside its line limit; voiceroutes.Register calls it, next to the routes of the
// reference voice of the same Characters.
package characterroutes

import (
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/handler/characterhandler"
)

// Register adds POST /projects/:id/characters and POST /series/:id/characters.
func Register(engine *gin.Engine, svc *service.Services) {
	engine.POST("/projects/:id/characters", characterhandler.CreateInProject(svc))
	engine.POST("/series/:id/characters", characterhandler.CreateInSeries(svc))
}
