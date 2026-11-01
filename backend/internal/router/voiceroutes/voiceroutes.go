// Package voiceroutes registers the routes of the reference voice of a Character (spec/api/endpoints.md §2.9): for the
// canonical Characters of a Series and for the Characters of a Project. The routes live outside router so that it
// stays inside its line limit; router.New calls Register.
package voiceroutes

import (
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/handler/voicehandler"
)

// Register adds the routes: GET plays the recording, PUT sets it (multipart) and DELETE removes it. Every PUT is an
// upload route of web/middleware (its own size limit).
func Register(engine *gin.Engine, svc *service.Services) {
	const series, project = "/series/:id/characters/:characterId/voice-reference", "/projects/:id/characters/:characterId/voice-reference"
	engine.GET(series, voicehandler.AudioSeries(svc))
	engine.PUT(series, voicehandler.SetSeries(svc))
	engine.DELETE(series, voicehandler.RemoveSeries(svc))
	engine.GET(project, voicehandler.AudioProject(svc))
	engine.PUT(project, voicehandler.SetProject(svc))
	engine.DELETE(project, voicehandler.RemoveProject(svc))
}
