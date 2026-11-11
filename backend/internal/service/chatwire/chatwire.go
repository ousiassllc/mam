// Package chatwire connects the chat operations of the global settings (service/chatops/*, #1140) to the services the app built: the same services the settings screens and
// the MCP tools use. Each operation's file adds a builder; Connect runs them once at start. It is a package of its own (service imports the contract, chatops, only) because
// service is at its size limit.
package chatwire

import (
	"errors"

	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/chatops"
	_ "github.com/ousiassllc/mam/backend/internal/service/chatops/outofscope" // registers the refusal of what is GUI-only on purpose (no handler of its own)
	"github.com/ousiassllc/mam/backend/internal/service/llmassign"
	"github.com/ousiassllc/mam/backend/internal/service/llmmodels"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
)

// Settings are the services of the global settings screen that service.Services does not hold (the app builds them next to the router). A nil field is a section the app
// does not have; its operation is not wired and the chat says it cannot be done from here.
type Settings struct {
	LLM       *llmassign.Service
	LLMModels *llmmodels.Service
	Models    *modelsettings.Service
}

// Env is what a chat operation is built from: the services as the app wired them, and the global settings.
type Env struct {
	*service.Services
	Settings Settings
}

// builders build the operations: each file adds its own in init, so wiring them is not a list in one place.
var builders []func(env Env, reg *chatops.Runtime)

// Connect wires the chat operations to the services. It MUST run after the app has set the late fields of Services (ModelManager, LocalRuntimes, ...) and once the global
// settings services exist. Without it the operations answer that they are not available.
func Connect(s *service.Services, settings Settings) {
	env, reg := Env{Services: s, Settings: settings}, s.ChatOps()
	for _, build := range builders {
		build(env, reg)
	}
}

// refusalOf turns the refusals of the service layer a user can act on (a field that is not valid, a settings file a start option fixed) into *chatops.Refusal, which the chat
// tells as it is. Any other error is returned as it is.
func refusalOf(err error) error {
	var (
		invalid *service.ValidationError
		model   *modelsettings.InvalidError
	)
	switch {
	case err == nil:
		return nil
	case errors.As(err, &invalid):
		return &chatops.Refusal{Message: invalid.Message}
	case errors.As(err, &model):
		return &chatops.Refusal{Message: model.Message}
	case errors.Is(err, modelsettings.ErrPinned):
		return &chatops.Refusal{Message: "起動オプションで設定ファイルが固定されているため、変更できません"}
	}
	return err
}
