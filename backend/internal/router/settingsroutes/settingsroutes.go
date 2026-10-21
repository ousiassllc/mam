// Package settingsroutes holds the routes of the global settings screen (paths.Settings) and the services
// behind its LLM / image and video model sections. They live outside router (and service) so that the
// two stay inside their line limits; router.Options.Settings hands them to router.New.
package settingsroutes

import (
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/appupdate"
	"github.com/ousiassllc/mam/backend/internal/service/llmassign"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
	"github.com/ousiassllc/mam/backend/internal/web/handler"
	updatehandler "github.com/ousiassllc/mam/backend/internal/web/handler/appupdate"
	llmhandler "github.com/ousiassllc/mam/backend/internal/web/handler/llmassign"
	modelhandler "github.com/ousiassllc/mam/backend/internal/web/handler/modelassign"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// Services are the services of the sections of the settings screen.
type Services struct {
	LLM    *llmassign.Service
	Models *modelsettings.Service
	// Update checks for a newer version. Nil in the headless server, which cannot update itself: the
	// version section then shows the version only and has no check route.
	Update *appupdate.Service
}

// New builds the services over the settings files (llm.json / models.json, or the files the operator pinned
// by a start option, which the screen shows and does not write).
func New(llmFile string, llmPinned bool, modelFile string, modelPinned bool) *Services {
	return &Services{LLM: llmassign.New(llmFile, llmPinned), Models: modelsettings.New(modelFile, modelPinned)}
}

// Register adds GET paths.Settings() (always: the API key panel needs only keys and the version section
// only the version) and, when s is not nil, the LLM and model sections with their PUT / DELETE routes and,
// when s.Update is set, POST paths.UpdateCheck(). Without s the screen has no such sections and the routes
// do not exist.
func Register(engine *gin.Engine, keys *service.CredentialService, s *Services) {
	var update *appupdate.Service
	if s != nil {
		update = s.Update
	}
	addSections := func(d *organisms.GlobalSettingsBodyData) {
		if s != nil {
			llm, models := llmhandler.SectionData(s.LLM), modelhandler.SectionData(s.Models)
			d.LLM, d.Models = &llm, &models
		}
		version := updatehandler.SectionData(update)
		d.Update = &version
	}
	if update != nil {
		engine.POST(paths.UpdateCheck(), updatehandler.Check(update))
	}
	if s != nil {
		engine.PUT(paths.LLMSettings(), llmhandler.Save(s.LLM))
		engine.DELETE(paths.LLMSettings(), llmhandler.Reset(s.LLM))
		engine.PUT(paths.ModelSettings(), modelhandler.Save(s.Models))
		engine.DELETE(paths.ModelSettings(), modelhandler.Reset(s.Models))
	}
	engine.GET(paths.Settings(), handler.GlobalSettingsPage(keys, addSections))
}
