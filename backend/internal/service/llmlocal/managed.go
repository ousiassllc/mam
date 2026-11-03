package llmlocal

import (
	"context"
	"errors"
)

// ErrManagedUnavailable means the user chose "この PC の llama.cpp を使う" but the app has no llama.cpp runtime (no local runtime manager is wired).
var ErrManagedUnavailable = errors.New("llmlocal: the managed llama.cpp runtime is not available")

// ManagedState is where the llama.cpp server that mam manages stands, for the card and the model list.
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

// ManagedInfo is what the card and the model list show of the managed server.
type ManagedInfo struct {
	State  ManagedState
	Reason string
	// Model is the id of the model in force (the name the server answers to: the llama-server --alias), ModelName its name for the screen; "" when the runtime has none.
	Model, ModelName string
}

// Managed is the llama.cpp server that mam installs, starts and stops itself (package llamaserver, the local runtime "llama-cpp"): the settings
// can make the provider "local" use it instead of an address.
type Managed interface {
	// Endpoint starts the server when it is stopped and returns its address and the key of this start; the error says why it cannot run now.
	Endpoint(ctx context.Context) (baseURL, key string, err error)
	// Stop stops the server (a stopped one is a no-op): it gives the video memory back before an image or video generation.
	Stop(ctx context.Context) error
	// Info says where the server stands, without starting it.
	Info(ctx context.Context) ManagedInfo
}

// SetManaged sets the llama.cpp server of mam. Without one the choice "この PC の llama.cpp を使う" is refused (ErrManagedUnavailable).
func (s *Service) SetManaged(m Managed) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.managed = m
}

func (s *Service) managedServer() Managed {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.managed
}

// InUse says whether the calls go to the managed llama.cpp server (the user chose it and the app has one). It implements localllm.ManagedServer.
func (s *Service) InUse() bool {
	return s.managedServer() != nil && s.settingsView().UseManaged
}

// Endpoint is the address and key of the managed server, started when it is stopped. It implements localllm.ManagedServer.
func (s *Service) Endpoint(ctx context.Context) (string, string, error) {
	m := s.managedServer()
	if m == nil {
		return "", "", ErrManagedUnavailable
	}
	return m.Endpoint(ctx)
}

// ManagedInfo says where the managed server stands (ok false: the app has none).
func (s *Service) ManagedInfo(ctx context.Context) (ManagedInfo, bool) {
	m := s.managedServer()
	if m == nil {
		return ManagedInfo{}, false
	}
	return m.Info(ctx), true
}

// ManagedModels is the model the managed server answers to, when the user chose it (managed false: the calls go to the address of the settings,
// so the model list asks that server). It lists the model in force even while the server is stopped: it starts at the first call.
func (s *Service) ManagedModels(ctx context.Context) (models []string, managed bool) {
	if !s.InUse() {
		return nil, false
	}
	info, _ := s.ManagedInfo(ctx)
	if info.Model == "" || info.State == ManagedUnsupported || info.State == ManagedNotInstalled {
		return nil, true
	}
	return []string{info.Model}, true
}
