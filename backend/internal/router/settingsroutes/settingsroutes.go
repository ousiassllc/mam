// Package settingsroutes holds the routes of the global settings screen (paths.Settings) and the services
// behind its LLM / image and video model sections. They live outside router (and service) so that the
// two stay inside their line limits; router.Options.Settings hands them to router.New.
package settingsroutes

import (
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/llmassign"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
	"github.com/ousiassllc/mam/backend/internal/web/handler"
	llmhandler "github.com/ousiassllc/mam/backend/internal/web/handler/llmassign"
	modelhandler "github.com/ousiassllc/mam/backend/internal/web/handler/modelassign"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// Services are the services of the assignment sections of the settings screen.
type Services struct {
	LLM    *llmassign.Service
	Models *modelsettings.Service
}

// New builds the services over the settings files (llm.json / models.json, or the files the operator pinned
// by a start option, which the screen shows and does not write).
func New(llmFile string, llmPinned bool, modelFile string, modelPinned bool) *Services {
	return &Services{LLM: llmassign.New(llmFile, llmPinned), Models: modelsettings.New(modelFile, modelPinned)}
}

// Register adds GET paths.Settings() (always: the API key panel needs only keys) and, when s is not nil, the
// LLM and model sections with their PUT / DELETE routes. Without s the screen has no such sections and the
// routes do not exist.
func Register(engine *gin.Engine, keys *service.CredentialService, s *Services) {
	var addSections func(*organisms.GlobalSettingsBodyData)
	if s != nil {
		addSections = func(d *organisms.GlobalSettingsBodyData) {
			llm, models := llmhandler.SectionData(s.LLM), modelhandler.SectionData(s.Models)
			d.LLM, d.Models = &llm, &models
		}
		engine.PUT(paths.LLMSettings(), llmhandler.Save(s.LLM))
		engine.DELETE(paths.LLMSettings(), llmhandler.Reset(s.LLM))
		engine.PUT(paths.ModelSettings(), modelhandler.Save(s.Models))
		engine.DELETE(paths.ModelSettings(), modelhandler.Reset(s.Models))
	}
	engine.GET(paths.Settings(), handler.GlobalSettingsPage(keys, addSections))
}
