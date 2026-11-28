package llmlocal

import (
	"context"
	"errors"

	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
)

// ErrManagedUnavailable means the user chose "この PC の llama.cpp" but the app has no llama.cpp runtime (no local runtime manager is wired).
var ErrManagedUnavailable = errors.New("llmlocal: the managed llama.cpp runtime is not available")

// ErrManagedOllamaUnavailable means the user chose "Ollama" but the app has no Ollama runtime that mam manages (#1105).
var ErrManagedOllamaUnavailable = errors.New("llmlocal: the managed Ollama runtime is not available")

// ManagedState is where a server that mam manages stands, for the card and the model list.
type ManagedState string

// The states of the managed server.
const (
	// ManagedUnsupported: this PC cannot run it (Reason says why, in Japanese, with what to use instead).
	ManagedUnsupported ManagedState = "unsupported"
	// ManagedNotInstalled: it is not installed, or the weights of the model in force are not all there (Reason says what to do).
	ManagedNotInstalled ManagedState = "not_installed"
	// ManagedStopped: it is installed and ready; it starts at the first call.
	ManagedStopped ManagedState = "stopped"
	// ManagedStarting: it is loading the model.
	ManagedStarting ManagedState = "starting"
	// ManagedRunning: it answers.
	ManagedRunning ManagedState = "running"
	// ManagedFailed: it could not start or died (Reason has the end of its log).
	ManagedFailed ManagedState = "failed"
)

// ManagedModel is one model the managed server offers (its catalog: every model of llama.cpp's list, every model of Ollama's), with what the model picker of the LLM assignment says of
// it (#1235): whether it is a model without refusals, its license and why it cannot be chosen now.
type ManagedModel struct {
	// ID is the name the server answers to ("gemma-4-12b-it" for llama.cpp, "gemma4:12b" for Ollama); Name what the screens call it.
	ID, Name string
	// Server is the use the model belongs to ("この PC の llama.cpp", "Ollama"), set by ManagedCatalog.
	Server string
	// Uncensored: the model's refusals were removed (an abliterated derivative).
	Uncensored bool
	// License is the license of the model with its answer for commercial use (Commercial Unknown, with the notice, when nothing says).
	License modellicense.Info
	// Reason says why the model cannot be chosen now, with what to do ("" when it can): the PC cannot run the server, the server or its weights are not installed, the model is not the
	// one in force (llama.cpp) or is not fetched (Ollama), or the calls go to another server (ManagedCatalog).
	Reason string
}

// ManagedInfo is what the card and the model list show of the managed server.
type ManagedInfo struct {
	State  ManagedState
	Reason string
	// Model is the id of the model in force (the name the server answers to: the llama-server --alias), ModelName its name for the screen; "" when the runtime has none.
	Model, ModelName string
	// Installing says an install of the server (or of its weights) is running: the state is then NotInstalled, and it is not a problem to be fixed.
	Installing bool
	// URL is the address the server answers at while it runs ("" otherwise); the managed Ollama is unloaded through it (BeforeGeneration).
	URL string
	// Models are the models the server answers to, when it holds several (the managed Ollama: every model it fetched); empty for a server that answers to Model only.
	Models []string
	// Catalog is every model of the server's list, chosen or not, with why one cannot be chosen now (ManagedCatalog); empty when the server has no list.
	Catalog []ManagedModel
}

// Managed is a server that mam installs, starts and stops itself (package llamaserver, the local runtime "llama-cpp"; the local runtime "ollama" of #1105):
// the settings can make the provider "local" use it instead of an address.
type Managed interface {
	// Endpoint starts the server when it is stopped and returns its address and the key of this start; the error says why it cannot run now.
	Endpoint(ctx context.Context) (baseURL, key string, err error)
	// Stop stops the server (a stopped one is a no-op): it gives the video memory back before an image or video generation.
	Stop(ctx context.Context) error
	// Info says where the server stands, without starting it.
	Info(ctx context.Context) ManagedInfo
}

// SetManaged sets the llama.cpp server of mam. Without one the choice "この PC の llama.cpp" is refused (ErrManagedUnavailable).
func (s *Service) SetManaged(m Managed) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.managed = m
}

// SetManagedOllama sets the Ollama server of mam (#1105). Without one the choice "Ollama" is refused (ErrManagedOllamaUnavailable) and the
// section "使うサーバー" does not offer it.
func (s *Service) SetManagedOllama(m Managed) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ollama = m
}

// managedFor is the server mam manages for the use (nil: the app has none, or the use is Custom).
func (s *Service) managedFor(u Use) Managed {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch u {
	case UseLlamaCpp:
		return s.managed
	case UseOllama:
		return s.ollama
	}
	return nil
}

// Offers says whether the app can send the calls to the use: Custom always, a managed server when the app has it.
func (s *Service) Offers(u Use) bool { return u == UseCustom || s.managedFor(u) != nil }

// activeManaged is the managed server the calls go to now (nil: they go to the address of the settings, or the user chose a server the app does not have).
func (s *Service) activeManaged() Managed { return s.managedFor(s.settingsView().Use) }

// InUse says whether the calls go to a managed server (the user chose it and the app has one). It implements localllm.ManagedServer.
func (s *Service) InUse() bool { return s.activeManaged() != nil }

// Endpoint is the address and key of the managed server in use, started when it is stopped. It implements localllm.ManagedServer.
func (s *Service) Endpoint(ctx context.Context) (string, string, error) {
	m := s.activeManaged()
	if m == nil {
		return "", "", ErrManagedUnavailable
	}
	return m.Endpoint(ctx)
}

// ManagedInfo says where the managed server of the use stands (the llama.cpp of mam while the calls go to an address; ok false: the app has none).
func (s *Service) ManagedInfo(ctx context.Context) (ManagedInfo, bool) {
	u := s.settingsView().Use
	if u == UseCustom {
		u = UseLlamaCpp
	}
	m := s.managedFor(u)
	if m == nil {
		return ManagedInfo{}, false
	}
	return m.Info(ctx), true
}

// ManagedModels are the models the managed server answers to, when the user chose it (managed false: the calls go to the address of the settings,
// so the model list asks that server): the model in force of llama.cpp, every model the managed Ollama holds. It lists them even while the server is stopped: it starts at the first call.
func (s *Service) ManagedModels(ctx context.Context) (models []string, managed bool) {
	if !s.InUse() {
		return nil, false
	}
	info, _ := s.ManagedInfo(ctx)
	models = info.Models
	if len(models) == 0 && info.Model != "" {
		models = []string{info.Model}
	}
	if len(models) == 0 || info.State == ManagedUnsupported || info.State == ManagedNotInstalled {
		return nil, true
	}
	return models, true
}

// ManagedCatalog is every model of the servers that mam manages (llama.cpp's list and Ollama's), each with why it cannot be chosen now, whichever server the calls go to: a PC that cannot
// run the server, a server or its weights that are not installed, a model that is not the one in force or not fetched, or "使うサーバー" set to another server. The picker of the LLM
// assignment lists them, so that a model that this PC hides is shown with its reason rather than missing (#1235).
func (s *Service) ManagedCatalog(ctx context.Context) []ManagedModel {
	active := s.settingsView().Use
	var out []ManagedModel
	for _, u := range []Use{UseLlamaCpp, UseOllama} {
		m := s.managedFor(u)
		if m == nil {
			continue
		}
		for _, cm := range m.Info(ctx).Catalog {
			cm.Server = u.Name()
			if cm.Reason == "" && active != u {
				cm.Reason = "今の「使うサーバー」は「" + active.Name() + "」です。モデルマネージャーの「ローカル LLM」で「" + u.Name() + "」を選ぶと使えます"
			}
			out = append(out, cm)
		}
	}
	return out
}
