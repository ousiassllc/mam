package app

import (
	"log/slog"
	"path/filepath"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/app/restartwork"
	"github.com/ousiassllc/mam/backend/internal/chatgpt"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex"
	"github.com/ousiassllc/mam/backend/internal/config"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/media/ffmpegdl"
	"github.com/ousiassllc/mam/backend/internal/media/ffwire"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/router/settingsroutes"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/facefusionsettings"
	"github.com/ousiassllc/mam/backend/internal/service/ffmpegsettings"
	"github.com/ousiassllc/mam/backend/internal/service/llmmodels"
	"github.com/ousiassllc/mam/backend/internal/service/onboarding"
	"github.com/ousiassllc/mam/backend/internal/service/restartpending"
)

// newSettings is the settings services of the screen and the MCP server (the Update and MCP parts are set by the caller).
// The age gate is wired here (SetHidden): a model that is an adult model while the age confirmation is off is left out of the
// assignment choices and refused when saved, by the same predicate that refuses a generation (buildServices, Gate.Set).
func newSettings(cfg config.Config, services *service.Services, gpt *chatgpt.Service, ffmpegDL *ffmpegdl.Service, restart func(), jobs *repository.JobRepository, logger *slog.Logger) *settingsroutes.Services {
	llmFile, llmPinned := cfg.LLMSettingsFile()
	modelFile, modelPinned := cfg.ModelSettingsFile()
	settings := settingsroutes.New(llmFile, llmPinned, modelFile, modelPinned)
	settings.LocalModels = services.LocalModels
	var pending *restartpending.Registry
	var codexSvc *codex.Service
	if gpt != nil {
		codexSvc = gpt.Codex()
	}
	settings.Restart, pending = restartwork.New(restart, services, jobs, ffmpegDL, codexSvc, logger)
	if services.LLMClient != nil { // a saved LLM assignment applies at once (restartwork.LLMApplier)
		apply := restartwork.LLMApplier(services.LLMClient, pending)
		settings.LLM.SetApplier(apply)
		if services.LLMLocal != nil {
			services.LLMLocal.SetApplier(apply) // so does the address of the local server
		}
	}
	settings.LLMLocal = services.LLMLocal
	look := newFFmpegSettings(cfg, ffmpegDL).Look
	ffmpeg := newFFmpegSettings(cfg, ffmpegDL, ffmpegsettings.WithOnSet(func() { ffwire.Reload(services.FFmpeg, look, logger) }), ffmpegsettings.WithOnView(ffwire.Shown(services.FFmpeg, look, logger)))
	settings.FFmpeg, settings.FFmpegDownload = ffmpeg, ffmpegDL
	settings.FaceFusion = facefusionsettings.Default(cfg.DataDir) // the external face swapper (#995); the generation test reads the same file
	settings.Keys = credential.NewKeyring()                       // the OS secure storage: the connection test of an API key reads a saved key from it
	settings.Workflows = services.Workflows
	// A saved image / video assignment applies at once, as a saved endpoint or model does.
	settings.Models.SetOnChange(func() {
		if err := services.LocalModels.Reload(); err != nil {
			logger.Error("apply the saved model settings failed", "err", err)
		}
	})
	settings.ChatGPT = gpt
	models := llmmodels.Options{Keys: settings.Keys, BaseURLs: llmBaseURLs(cfg, logger), Logger: logger}
	if services.LLMLocal != nil {
		models.LocalBaseURL = services.LLMLocal.BaseURL       // the address can change while the app runs
		models.LocalManaged = services.LLMLocal.ManagedModels // and the user can choose mam's own llama.cpp instead
	}
	if gpt != nil {
		models.ChatGPT = llmmodels.FromChatGPT(gpt)
	}
	settings.LLMModels = llmmodels.New(models)
	settings.ModelManager, settings.LocalRuntimes = services.ModelManager, services.LocalRuntimes
	settings.ModelNodes = services.ModelNodes
	settings.Audio = newAudioSettings(cfg, services.LocalRuntimes, settings.Keys)
	settings.Models.SetHidden(services.ModelManager.HidesModel)
	settings.Gentest = services.Gentest
	if services.Gentest != nil { // the choices of the assignment say which models cannot be used now (#1013), by the judgement the generation test lists them with
		settings.Models.SetAvailability(services.Gentest.Availability)
	}
	settings.Onboarding = onboarding.New(filepath.Join(cfg.DataDir, onboarding.FileName), onboarding.WithLookPath(ffmpeg.Find))
	return settings
}

// llmBaseURLs is the base_url of each provider as llm.json sets it: the model list's API key goes where the Agents' calls go.
// A file that cannot be read gives none (the defaults).
func llmBaseURLs(cfg config.Config, logger *slog.Logger) map[llm.Provider]string {
	file, err := settingsFileToLoad(cfg.LLMSettingsFile())
	if err == nil {
		var loaded llm.Config
		if loaded, err = llm.LoadConfig(file); err == nil {
			urls := make(map[llm.Provider]string, len(loaded.Providers))
			for p, pc := range loaded.Providers {
				urls[p] = pc.BaseURL
			}
			return urls
		}
	}
	logger.Warn("llm.json is not read for the base_url of the model lists; the vendors' own are used", "err", err)
	return nil
}
