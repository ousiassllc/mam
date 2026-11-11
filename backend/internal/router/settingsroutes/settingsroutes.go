// Package settingsroutes holds the routes of the global settings screen (paths.Settings) and the services
// behind its LLM / image and video model sections. They live outside router (and service) so that the
// two stay inside their line limits; router.Options.Settings hands them to router.New.
package settingsroutes

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/chatgpt"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/credential/probe"
	"github.com/ousiassllc/mam/backend/internal/media/ffmpegdl"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/apprestart"
	"github.com/ousiassllc/mam/backend/internal/service/appupdate"
	"github.com/ousiassllc/mam/backend/internal/service/audiosettings"
	"github.com/ousiassllc/mam/backend/internal/service/facefusionsettings"
	"github.com/ousiassllc/mam/backend/internal/service/ffmpegsettings"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	gentestchatsvc "github.com/ousiassllc/mam/backend/internal/service/gentest/chat"
	"github.com/ousiassllc/mam/backend/internal/service/joblist"
	"github.com/ousiassllc/mam/backend/internal/service/llmassign"
	"github.com/ousiassllc/mam/backend/internal/service/llmlocal"
	"github.com/ousiassllc/mam/backend/internal/service/llmmodels"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
	"github.com/ousiassllc/mam/backend/internal/service/mcpsettings"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelnodes"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
	"github.com/ousiassllc/mam/backend/internal/service/ollamapull"
	onboardingsvc "github.com/ousiassllc/mam/backend/internal/service/onboarding"
	workflowsvc "github.com/ousiassllc/mam/backend/internal/service/workflows"
	updatehandler "github.com/ousiassllc/mam/backend/internal/web/handler/appupdate"
	chatgpthandler "github.com/ousiassllc/mam/backend/internal/web/handler/chatgptsettings"
	gentesthandler "github.com/ousiassllc/mam/backend/internal/web/handler/gentest"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/chatscreen"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/comfyscreen"
	jobshandler "github.com/ousiassllc/mam/backend/internal/web/handler/jobs"
	modelmgrhandler "github.com/ousiassllc/mam/backend/internal/web/handler/modelmgr"
	companionhandler "github.com/ousiassllc/mam/backend/internal/web/handler/modelmgr/companion"
	ffmpeghandler "github.com/ousiassllc/mam/backend/internal/web/handler/modelmgr/companion/ffmpeg"
	"github.com/ousiassllc/mam/backend/internal/web/handler/modelpicker"
	onboardinghandler "github.com/ousiassllc/mam/backend/internal/web/handler/onboarding"
	audiohandler "github.com/ousiassllc/mam/backend/internal/web/handler/settings/audio"
	credentialshandler "github.com/ousiassllc/mam/backend/internal/web/handler/settings/credentials"
	llmhandler "github.com/ousiassllc/mam/backend/internal/web/handler/settings/llm"
	localhandler "github.com/ousiassllc/mam/backend/internal/web/handler/settings/local"
	mcphandler "github.com/ousiassllc/mam/backend/internal/web/handler/settings/mcp"
	mediahandler "github.com/ousiassllc/mam/backend/internal/web/handler/settings/media"
	restarthandler "github.com/ousiassllc/mam/backend/internal/web/handler/settings/restart"
	"github.com/ousiassllc/mam/backend/internal/web/handler/settingspage"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// Services are the services of the sections of the settings screen.
type Services struct {
	LLM *llmassign.Service
	// LLMModels are the models the LLM pull-downs offer for each provider (the API's list, else the candidates of mam). New
	// builds one that knows no key and no sign-in (the candidates only); the app replaces it.
	LLMModels *llmmodels.Service
	// LLMLocal is the local OpenAI-compatible LLM server (its address and key, #1008; the server the calls of the provider "local" go to, #1104): the page "ローカル LLM" of the model manager and the card of the settings that links to it. New builds one that has no key
	// store (the key cannot be kept); the app replaces it.
	LLMLocal *llmlocal.Service
	Models   *modelsettings.Service
	// Update checks for a newer version. Nil in the headless server, which cannot update itself: the
	// version section then shows the version only and has no check route.
	Update *appupdate.Service
	// Restart is the "再起動して反映" panel: the settings that wait for a restart and the button (POST paths.Restart()). Nil leaves
	// the panel and its route out.
	Restart *apprestart.Service
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
	// LocalRuntimes are the local voice and lip-sync runtimes (GPT-SoVITS, MuseTalk) shown in the model manager screen. Nil leaves the
	// section "ローカル音声・口パク" out.
	LocalRuntimes *localruntime.Service
	// ModelNodes are the custom nodes of the allowlist (section "追加ノード" of the model manager screen). Nil leaves the section out.
	ModelNodes *modelnodes.Service
	// Ollama are the models of the Ollama that mam manages (section "Ollama のモデル" of the page "ローカル LLM", #1105). Nil leaves the section and its routes out.
	Ollama *ollamapull.Service
	// Onboarding is the first-run guide (paths.Onboarding) and the redirect of a first run from GET / (OnboardingHome). Nil
	// leaves both out: the root page is always the project list.
	Onboarding *onboardingsvc.Service
	// Audio is the "音声・口パク" section (the choice of the voice and lip-sync providers). Nil leaves the section and its route out.
	Audio *audiosettings.Service
	// FFmpeg is the section "ffmpeg" of the companion software of the model manager (the paths of ffmpeg and ffprobe; #1106). Nil leaves the section and its routes out.
	FFmpeg *ffmpegsettings.Service
	// FFmpegDownload is the download of ffmpeg and ffprobe (the button of the ffmpeg section and of the first-run guide). Nil
	// leaves the download and its routes out.
	FFmpegDownload *ffmpegdl.Service
	// FaceFusion is the section "FaceFusion" of the companion software (the paths of the external face swapper, and how to install it). Nil leaves the section and its routes out.
	FaceFusion *facefusionsettings.Service
	// Gentest is the generation test screen (paths.GenerationTest; service.Services.Gentest) and the face-check models of the model manager
	// screen. Nil leaves the screen and that section out.
	Gentest *gentest.Service
	// GentestChat is the chat of the single generation screen (paths.GenerationTestChat, #1138) when a test brings its own; nil builds it from the LLM client of the
	// app's services (the route of the Prompt Planner), and leaves the page a notice and its routes out when there is none.
	GentestChat *gentestchatsvc.Service
	// JobList is the job list screen (paths.Jobs): the jobs that run and the latest that ended. Nil leaves the screen out.
	JobList *joblist.Service
}

// New builds the services over the settings files (llm.json / models.json, or the files the operator pinned
// by a start option, which the screen shows and does not write).
func New(llmFile string, llmPinned bool, modelFile string, modelPinned bool) *Services {
	return &Services{
		LLM: llmassign.New(llmFile, llmPinned), LLMModels: llmmodels.New(llmmodels.Options{}), LLMLocal: llmlocal.New(llmFile, llmPinned, nil, nil),
		Models: modelsettings.New(modelFile, modelPinned),
	}
}

// registerRestart adds the routes of the "再起動して反映" panel when the app has a restart service.
func registerRestart(engine *gin.Engine, s *Services) {
	if s != nil && s.Restart != nil {
		engine.GET(paths.Restart(), restarthandler.Show(s.Restart))
		engine.POST(paths.Restart(), restarthandler.Restart(s.Restart))
	}
}

// Register adds the pages of the settings screen (settingspage.Register: the overview and "API キーとログイン" always,
// the version page with the version only, and the other categories of the services there are) and, when s is not nil, the
// LLM and model sections with their PUT / DELETE routes and, when s.Update is set, POST paths.UpdateCheck() and, when s.MCP
// is set, the MCP server section with its PUT / POST / DELETE routes. Without s the screen has no such sections and the
// routes do not exist. It also adds the status route of the model manager card of the settings screen (registerTools).
func Register(engine *gin.Engine, services *service.Services, s *Services, logger *slog.Logger) {
	pages := pageServices(services, s)
	update := pages.Update
	credentialshandler.Register(engine, credentialshandler.Keys{Svc: services.Credentials, Tester: pages.Tester, Models: pages.Models}, pages.Page(paths.SettingsAPIKeys))
	if update != nil {
		engine.POST(paths.UpdateCheck(), updatehandler.Check(update))
	}
	registerRestart(engine, s)
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
		engine.GET(paths.ChatGPTDetail(":kind"), chatgpthandler.Detail(s.ChatGPT, apiKeys))
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
		llmhandler.Register(engine, s.LLM, s.LLMModels, pages.Page(paths.SettingsLLM))
		mediahandler.Register(engine, s.Models, mediahandler.Cards{Gentest: s.Gentest}, pages.Page(paths.SettingsModels))
		modelpicker.Register(engine, modelpicker.Services{Gentest: s.Gentest, Settings: s.Models, LLM: s.LLMModels, ModelManager: s.ModelManager})
	}
	if s != nil && s.Audio != nil {
		engine.GET(paths.AudioDetail(":choice", ":provider"), audiohandler.Detail(s.Audio, pages.Page(paths.SettingsAudio)))
		engine.PUT(paths.AudioSettings(), audiohandler.Save(s.Audio))
	}
	registerRetiredCategories(engine)
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
		modelmgrhandler.Register(engine, s.ModelManager, s.Gentest, s.LocalRuntimes, s.ModelNodes, s.LLMLocal, s.Ollama, companionServices(s))
	}
	if s != nil && s.Gentest != nil {
		gentesthandler.Register(engine, s.Gentest)
		// #1138: the chat of the single generation (its page says so when the app has no LLM for it)
		chatscreen.Register(engine, s.Gentest, chatOf(services, s))
		comfyscreen.Register(engine, s.Gentest, s.Workflows, s.LocalModels, gentesthandler.RequestOfForm(s.Gentest)) // #977: the ComfyUI tier
	}
	if s != nil && s.JobList != nil {
		jobshandler.Register(engine, s.JobList)
	}
	if s != nil && s.Onboarding != nil {
		registerOnboarding(engine, services, s, pages)
	}
	settingspage.Register(engine, pages)
	registerTools(engine, s, logger)
}

// registerRetiredCategories sends the old pages of the categories "ffmpeg" and "FaceFusion" of the settings screen (a bookmark, a link in an old message) to the
// companion software of the model manager (#1106): 303, with no fragment. It is a middleware, not a route, so that no route is left at those paths and their writes
// (PUT, POST, DELETE, and the download under /settings/ffmpeg) are 404 as if they had never been, not 405.
func registerRetiredCategories(engine *gin.Engine) {
	retired := map[string]bool{
		paths.SettingsCategory(paths.RetiredSettingsFFmpeg): true, paths.SettingsCategory(paths.RetiredSettingsFaceFusion): true,
	}
	engine.Use(func(c *gin.Context) {
		if c.Request.Method == http.MethodGet && retired[c.Request.URL.Path] {
			c.Redirect(http.StatusSeeOther, paths.ModelManagerCategory(paths.MMCompanion))
			c.Abort()
			return
		}
		c.Next()
	})
}

// companionServices are the services of the companion software of the model manager: ffmpeg and its download, FaceFusion and the Codex engine (read for the list).
func companionServices(s *Services) companionhandler.Services {
	c := companionhandler.Services{
		FFmpeg: ffmpeghandler.Services{Settings: s.FFmpeg, Download: s.FFmpegDownload}, FaceFusion: s.FaceFusion,
	}
	if s.ChatGPT != nil {
		c.Codex = s.ChatGPT.Codex()
	}
	return c
}

// pageServices are the services the pages of the settings screen are built from (the ones s does not have stay nil).
func pageServices(services *service.Services, s *Services) settingspage.Services {
	p := settingspage.Services{Keys: services.Credentials}
	if s != nil {
		if s.Keys != nil {
			p.Tester = probe.NewTester(s.Keys)
		}
		p.ChatGPT, p.LLM, p.LLMModels, p.Models, p.LocalModels, p.MCP, p.Update, p.FFmpeg = s.ChatGPT, s.LLM, s.LLMModels, s.Models, s.LocalModels, s.MCP, s.Update, s.FFmpeg
		p.Audio, p.LLMLocal = s.Audio, s.LLMLocal
		p.Restart = s.Restart
		p.FaceFusion = s.FaceFusion
		p.Guide = s.Onboarding != nil
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

// OnboardingHome wraps the handler of GET / so that a first run is sent to the guide; without the guide (s or s.Onboarding
// nil) it is home itself.
func OnboardingHome(s *Services, home gin.HandlerFunc) gin.HandlerFunc {
	if s == nil || s.Onboarding == nil {
		return home
	}
	return onboardinghandler.FirstRun(s.Onboarding, home)
}

// registerOnboarding adds the routes of the first-run guide (spec/api/endpoints-onboarding.md).
func registerOnboarding(engine *gin.Engine, services *service.Services, s *Services, pages settingspage.Services) {
	h := onboardinghandler.Handlers{
		Guide: s.Onboarding, Keys: services.Credentials, Tester: pages.Tester, ChatGPT: s.ChatGPT, LLM: s.LLM, LLMModels: s.LLMModels, Models: s.Models,
		FFmpeg: ffmpeghandler.Services{Settings: s.FFmpeg, Download: s.FFmpegDownload},
	}
	engine.GET(paths.Onboarding(), h.Start)
	engine.GET(paths.OnboardingCredentialsStatus(), h.CredentialsStatus)
	engine.GET(paths.OnboardingStep(":step"), h.Step)
	engine.POST(paths.OnboardingFinish(), h.Finish)
	engine.PUT(paths.OnboardingCodexPath(), h.CodexPath)
}
