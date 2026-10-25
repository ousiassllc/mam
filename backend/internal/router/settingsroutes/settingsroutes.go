// Package settingsroutes holds the routes of the global settings screen (paths.Settings) and the services
// behind its LLM / image and video model sections. They live outside router (and service) so that the
// two stay inside their line limits; router.Options.Settings hands them to router.New.
package settingsroutes

import (
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/chatgpt"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/credential/probe"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/appupdate"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/llmassign"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/service/mcpsettings"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
	workflowsvc "github.com/ousiassllc/mam/backend/internal/service/workflows"
	updatehandler "github.com/ousiassllc/mam/backend/internal/web/handler/appupdate"
	chatgpthandler "github.com/ousiassllc/mam/backend/internal/web/handler/chatgptsettings"
	gentesthandler "github.com/ousiassllc/mam/backend/internal/web/handler/gentest"
	llmhandler "github.com/ousiassllc/mam/backend/internal/web/handler/llmassign"
	modelhandler "github.com/ousiassllc/mam/backend/internal/web/handler/modelassign"
	modelmgrhandler "github.com/ousiassllc/mam/backend/internal/web/handler/modelmgr"
	credentialshandler "github.com/ousiassllc/mam/backend/internal/web/handler/settings/credentials"
	localhandler "github.com/ousiassllc/mam/backend/internal/web/handler/settings/local"
	mcphandler "github.com/ousiassllc/mam/backend/internal/web/handler/settings/mcp"
	"github.com/ousiassllc/mam/backend/internal/web/handler/settingspage"
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
	// Keys is the key store; with it the API keys can be tried against their providers (the connection test). Nil leaves
	// the test out: the cards say that the first generation is the test.
	Keys credential.Store
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

// Register adds the pages of the settings screen (settingspage.Register: the overview and "API キーとログイン" always,
// the version page with the version only, and the other categories of the services there are) and, when s is not nil, the
// LLM and model sections with their PUT / DELETE routes and, when s.Update is set, POST paths.UpdateCheck() and, when s.MCP
// is set, the MCP server section with its PUT / POST / DELETE routes. Without s the screen has no such sections and the
// routes do not exist. It also adds the status routes of the tool cards of the home page (registerTools), which read the
// services of both.
func Register(engine *gin.Engine, services *service.Services, s *Services, logger *slog.Logger) {
	pages := pageServices(services, s)
	update := pages.Update
	credentialshandler.Register(engine, credentialshandler.Keys{Svc: services.Credentials, Tester: pages.Tester}, pages.Page(paths.SettingsAPIKeys))
	if update != nil {
		engine.POST(paths.UpdateCheck(), updatehandler.Check(update))
	}
	if s != nil && s.MCP != nil {
		h := mcphandler.Handlers{Svc: s.MCP, Page: pages.Page(paths.SettingsMCP)}
		engine.GET(paths.MCPBindEdit(), h.EditBind)
		engine.GET(paths.MCPTokenNew(), h.NewToken)
		engine.PUT(paths.MCPBind(), h.Bind)
		engine.POST(paths.MCPTokens(), h.Issue)
		engine.DELETE(paths.MCPToken(":id"), h.Revoke)
	}
	if s != nil && s.ChatGPT != nil {
		apiKeys := pages.Page(paths.SettingsAPIKeys)
		engine.GET(paths.ChatGPTSettings(), chatgpthandler.Panel(s.ChatGPT, apiKeys))
		engine.GET(paths.ChatGPTModels(), chatgpthandler.Models(s.ChatGPT, apiKeys))
		engine.GET(paths.ChatGPTLogin(":kind"), chatgpthandler.LoginModal(s.ChatGPT, apiKeys))
		engine.PUT(paths.ChatGPTImageEnabled(), chatgpthandler.SetImageEnabled(s.ChatGPT))
		engine.POST(paths.ChatGPTLogin(":kind"), chatgpthandler.StartLogin(s.ChatGPT))
		engine.POST(paths.ChatGPTLoginCancel(":kind"), chatgpthandler.CancelLogin(s.ChatGPT))
		engine.DELETE(paths.ChatGPTAccount(":kind"), chatgpthandler.Logout(s.ChatGPT))
		engine.GET(paths.ChatGPTCodexModels(), chatgpthandler.CodexModels(s.ChatGPT, apiKeys))
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
		registerLocalModels(engine, localhandler.Handlers{
			Local: s.LocalModels, Category: pages.Page(paths.SettingsLocalModels), Endpoint: pages.LocalEndpoint(),
		})
		engine.GET(paths.LocalModelsHealth(), settingspage.Health(s.LocalModels, 0))
	}
	if s != nil && s.ModelManager != nil {
		modelmgrhandler.Register(engine, s.ModelManager, s.Gentest)
	}
	if s != nil && s.Gentest != nil {
		gentesthandler.Register(engine, s.Gentest)
	}
	settingspage.Register(engine, pages)
	registerTools(engine, services, s, logger)
}

// pageServices are the services the pages of the settings screen are built from (the ones s does not have stay nil).
func pageServices(services *service.Services, s *Services) settingspage.Services {
	p := settingspage.Services{Keys: services.Credentials}
	if s != nil {
		if s.Keys != nil {
			p.Tester = probe.NewTester(s.Keys)
		}
		p.ChatGPT, p.LLM, p.Models, p.LocalModels, p.MCP, p.Update = s.ChatGPT, s.LLM, s.Models, s.LocalModels, s.MCP, s.Update
	}
	return p
}

// registerLocalModels adds the routes of the "ローカル・代替モデル" section (spec/api/endpoints.md).
func registerLocalModels(engine *gin.Engine, h localhandler.Handlers) {
	engine.GET(paths.LocalEndpoints()+"/new", h.NewEndpoint)
	engine.GET(paths.LocalEndpoint(":name"), h.Endpoint)
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
