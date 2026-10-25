package app

import (
	"log/slog"

	"github.com/ousiassllc/mam/backend/internal/chatgpt"
	"github.com/ousiassllc/mam/backend/internal/config"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/router/settingsroutes"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// newSettings is the settings services of the screen and the MCP server (the Update and MCP parts are set by the caller).
// The age gate is wired here (SetHidden): a model that is an adult model while the age confirmation is off is left out of the
// assignment choices and refused when saved, by the same predicate that refuses a generation (buildServices, Gate.Set).
func newSettings(cfg config.Config, services *service.Services, gpt *chatgpt.Service, logger *slog.Logger) *settingsroutes.Services {
	llmFile, llmPinned := cfg.LLMSettingsFile()
	modelFile, modelPinned := cfg.ModelSettingsFile()
	settings := settingsroutes.New(llmFile, llmPinned, modelFile, modelPinned)
	settings.LocalModels = services.LocalModels
	settings.Keys = credential.NewKeyring() // the OS secure storage: the connection test of an API key reads a saved key from it
	settings.Workflows = services.Workflows
	// A saved image / video assignment applies at once, as a saved endpoint or model does.
	settings.Models.SetOnChange(func() {
		if err := services.LocalModels.Reload(); err != nil {
			logger.Error("apply the saved model settings failed", "err", err)
		}
	})
	settings.ChatGPT = gpt
	settings.ModelManager = services.ModelManager
	settings.Models.SetHidden(services.ModelManager.HidesModel)
	settings.Gentest = services.Gentest
	return settings
}
