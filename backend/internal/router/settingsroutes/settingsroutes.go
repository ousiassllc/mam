// Package settingsroutes holds the routes of the global settings screen (paths.Settings) and the services
// behind its LLM / image and video model sections. They live outside router (and service) so that the
// two stay inside their line limits; router.Options.Settings hands them to router.New.
package settingsroutes

import (
	"context"
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/chatgpt"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/appupdate"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/llmassign"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/service/mcpsettings"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
	workflowsvc "github.com/ousiassllc/mam/backend/internal/service/workflows"
	"github.com/ousiassllc/mam/backend/internal/web/handler"
	updatehandler "github.com/ousiassllc/mam/backend/internal/web/handler/appupdate"
	chatgpthandler "github.com/ousiassllc/mam/backend/internal/web/handler/chatgptsettings"
	gentesthandler "github.com/ousiassllc/mam/backend/internal/web/handler/gentest"
	llmhandler "github.com/ousiassllc/mam/backend/internal/web/handler/llmassign"
	localhandler "github.com/ousiassllc/mam/backend/internal/web/handler/localmodels"
	mcphandler "github.com/ousiassllc/mam/backend/internal/web/handler/mcpsettings"
	modelhandler "github.com/ousiassllc/mam/backend/internal/web/handler/modelassign"
	modelmgrhandler "github.com/ousiassllc/mam/backend/internal/web/handler/modelmgr"
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
	// MCP is the MCP server section (address, bind address, tokens). Nil when the MCP server is not running
	// (a router built without the app, as the tests do): the section and its routes are then absent.
	MCP *mcpsettings.Service
	// LocalModels is the "ローカル・代替モデル" section (endpoints and models, service.Services.LocalModels).
	// Nil leaves the section and its routes out.
	LocalModels *localmodels.Service
	// ChatGPT is the ChatGPT sign-in section. Nil leaves the section and its routes out.
	ChatGPT *chatgpt.Service
	// Workflows is the node screen and the saved ComfyUI workflows (service.Services.Workflows). Nil leaves them out.
	Workflows *workflowsvc.Service
	// ModelManager is the model manager screen (paths.ModelManager). Nil leaves the screen and its routes out.
	ModelManager *modelmanager.Service
	// Gentest is the generation test screen (paths.GenerationTest) and the trial generations of the model manager screen
	// (service.Services.Gentest). Nil leaves the screen out and the trial panel without a form.
	Gentest *gentest.Service
}

// New builds the services over the settings files (llm.json / models.json, or the files the operator pinned
// by a start option, which the screen shows and does not write).
func New(llmFile string, llmPinned bool, modelFile string, modelPinned bool) *Services {
	return &Services{LLM: llmassign.New(llmFile, llmPinned), Models: modelsettings.New(modelFile, modelPinned)}
}

// Register adds GET paths.Settings() (always: the API key panel needs only keys and the version section
// only the version) and, when s is not nil, the LLM and model sections with their PUT / DELETE routes and,
// when s.Update is set, POST paths.UpdateCheck() and, when s.MCP is set, the MCP server section with its PUT / POST /
// DELETE routes. Without s the screen has no such sections and the routes do not exist. It also adds the status routes
// of the tool cards of the home page (registerTools), which read the services of both.
func Register(engine *gin.Engine, services *service.Services, s *Services, logger *slog.Logger) {
	keys := services.Credentials
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
		if s != nil && s.MCP != nil {
			mcp := mcphandler.SectionData(context.Background(), s.MCP)
			d.MCP = &mcp
		}
		if s != nil && s.LocalModels != nil {
			local := localhandler.SectionData(s.LocalModels)
			d.Local = &local
		}
		if s != nil && s.ChatGPT != nil {
			gpt := chatgpthandler.SectionData(s.ChatGPT)
			d.Credentials.ChatGPT = &gpt
		}
	}
	if update != nil {
		engine.POST(paths.UpdateCheck(), updatehandler.Check(update))
	}
	if s != nil && s.MCP != nil {
		engine.PUT(paths.MCPBind(), mcphandler.Bind(s.MCP))
		engine.POST(paths.MCPTokens(), mcphandler.Issue(s.MCP))
		engine.DELETE(paths.MCPToken(":id"), mcphandler.Revoke(s.MCP))
	}
	if s != nil && s.ChatGPT != nil {
		engine.GET(paths.ChatGPTSettings(), chatgpthandler.Panel(s.ChatGPT))
		engine.GET(paths.ChatGPTModels(), chatgpthandler.Models(s.ChatGPT))
		engine.PUT(paths.ChatGPTImageEnabled(), chatgpthandler.SetImageEnabled(s.ChatGPT))
		engine.POST(paths.ChatGPTLogin(":kind"), chatgpthandler.StartLogin(s.ChatGPT))
		engine.POST(paths.ChatGPTLoginCancel(":kind"), chatgpthandler.CancelLogin(s.ChatGPT))
		engine.DELETE(paths.ChatGPTAccount(":kind"), chatgpthandler.Logout(s.ChatGPT))
		engine.GET(paths.ChatGPTCodexModels(), chatgpthandler.CodexModels(s.ChatGPT))
		engine.POST(paths.ChatGPTCodexRefresh(), chatgpthandler.CodexRefresh(s.ChatGPT))
		engine.POST(paths.ChatGPTCodexInstall(), chatgpthandler.CodexInstall(s.ChatGPT))
		engine.POST(paths.ChatGPTCodexInstallCancel(), chatgpthandler.CodexInstallCancel(s.ChatGPT))
		engine.PUT(paths.ChatGPTCodexPath(), chatgpthandler.CodexSetPath(s.ChatGPT))
	}
	if s != nil {
		engine.PUT(paths.LLMSettings(), llmhandler.Save(s.LLM))
		engine.DELETE(paths.LLMSettings(), llmhandler.Reset(s.LLM))
		engine.PUT(paths.ModelSettings(), modelhandler.Save(s.Models))
		engine.DELETE(paths.ModelSettings(), modelhandler.Reset(s.Models))
	}
	if s != nil && s.Workflows != nil {
		registerWorkflows(engine, s.Workflows)
	}
	if s != nil && s.LocalModels != nil {
		registerLocalModels(engine, localhandler.Handlers{Local: s.LocalModels, Models: s.Models})
	}
	if s != nil && s.ModelManager != nil {
		modelmgrhandler.Register(engine, s.ModelManager, s.Gentest)
	}
	if s != nil && s.Gentest != nil {
		gentesthandler.Register(engine, s.Gentest)
	}
	engine.GET(paths.Settings(), handler.GlobalSettingsPage(keys, addSections))
	registerTools(engine, services, s, logger)
}

// registerLocalModels adds the routes of the "ローカル・代替モデル" section (spec/api/endpoints.md).
func registerLocalModels(engine *gin.Engine, h localhandler.Handlers) {
	engine.GET(paths.LocalModels(), h.Panel)
	engine.GET(paths.LocalEndpoints()+"/new", h.NewEndpoint)
	engine.POST(paths.LocalEndpoints(), h.CreateEndpoint)
	engine.POST(paths.LocalEndpoints()+"/test", h.TestDraft)
	engine.GET(paths.LocalEndpoint(":name", "edit"), h.EditEndpoint)
	engine.PUT(paths.LocalEndpoint(":name"), h.UpdateEndpoint)
	engine.DELETE(paths.LocalEndpoint(":name"), h.DeleteEndpoint)
	engine.POST(paths.LocalEndpoint(":name", "test"), h.TestEndpoint)
	engine.PUT(paths.LocalEndpoint(":name", "key"), h.SetKey)
	engine.DELETE(paths.LocalEndpoint(":name", "key"), h.ClearKey)
	engine.GET(paths.LocalEndpoint(":name", "models", "new"), h.NewModel)
	engine.POST(paths.LocalEndpoint(":name", "models", "start"), h.StartModel)
	engine.POST(paths.LocalEndpoint(":name", "models"), h.CreateModel)
	engine.GET(paths.LocalEndpoint(":name", "models", ":model", "edit"), h.EditModel)
	engine.PUT(paths.LocalEndpoint(":name", "models", ":model"), h.UpdateModel)
	engine.DELETE(paths.LocalEndpoint(":name", "models", ":model"), h.DeleteModel)
}
