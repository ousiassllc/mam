// Package voiceroutes registers the routes of the reference voice of a Character (spec/api/endpoints.md §2.9): for the
// canonical Characters of a Series and for the Characters of a Project. The routes live outside router so that it
// stays inside its line limit; router.New calls Register.
package voiceroutes

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/router/characterroutes"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/handler/voicehandler"
	"github.com/ousiassllc/mam/backend/internal/web/handler/voicetrainhandler"
)

// training adds the routes of the voice training panel of a Character below its voice-reference route: GET is the panel, POST starts a training,
// /cancel stops it, /clips adds a clip (an upload route of web/middleware) and /clips/:clipId and /voice remove one clip and the trained voice.
func training(engine *gin.Engine, svc *service.Services, voiceReference string, t voicetrainhandler.Target) {
	base := strings.TrimSuffix(voiceReference, "voice-reference") + "voice-training"
	engine.GET(base, t.Panel(svc))
	engine.POST(base, t.Start(svc))
	engine.POST(base+"/cancel", t.Cancel(svc))
	engine.POST(base+"/clips", t.AddClip(svc))
	engine.DELETE(base+"/clips", t.RemoveClip(svc))
	engine.DELETE(base+"/clips/:clipId", t.RemoveClip(svc))
	engine.DELETE(base+"/voice", t.RemoveTrained(svc))
}

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
	training(engine, svc, series, voicetrainhandler.Series)
	training(engine, svc, project, voicetrainhandler.Project)
	characterroutes.Register(engine, svc) // making a Character by hand (the screens' "登場人物を追加")
}
