package service

import (
	"errors"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/agent/jev"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/credential"
)

// MaxAPIKeyBytes bounds a pasted API key (within the Windows Credential Manager limit of 2560 bytes).
const MaxAPIKeyBytes = credential.MaxAPIKeyBytes

// CredentialProviders are the providers whose API key the settings screen manages, in display
// order. The ID is the credential.Store key the adapters read.
var CredentialProviders = []struct{ ID, Label string }{
	{string(llm.ProviderAnthropic), "Anthropic（Claude）"},
	{string(llm.ProviderOpenAI), "OpenAI"},
	{string(llm.ProviderGoogle), "Google（Gemini / Nano Banana）"},
	{string(modeladapter.VendorFlux), "Black Forest Labs（Flux）"},
	{string(modeladapter.VendorKling), "Kling"}, {string(modeladapter.VendorKling4), "Kling 4.0"},
	{string(modeladapter.VendorSeedance), "Seedance"}, {string(modeladapter.VendorRunway), "Runway"},
	{string(modeladapter.VendorMiniMax), "MiniMax（H3）"},
	{string(modeladapter.VendorAlibaba), "Alibaba Cloud（Wan 3.0）"}, {string(modeladapter.VendorLuma), "Luma（Ray 3.2）"},
	{audiodirector.CredentialProvider, "ElevenLabs"},
	{audiodirector.SyncCredentialProvider, "Sync Labs"},
	{jev.CredentialProvider, "TypeSafe（Jev）"},
}

// CredentialStatus is whether the key of a provider is set. It never carries the key.
type CredentialStatus struct {
	Provider   string
	Label      string
	Configured bool
}

// CredentialService manages external model API keys for the settings screen.
//
// It deliberately has no method that returns a stored key: the UI can only learn
// whether a key is configured, so a key can never reach the front end. Model
// Adapters read keys directly from credential.Store.
type CredentialService struct {
	store credential.Store
}

// NewCredentialService returns a CredentialService over store.
func NewCredentialService(store credential.Store) *CredentialService {
	return &CredentialService{store: store}
}

// Status lists every managed provider with whether its key is set.
func (s *CredentialService) Status() ([]CredentialStatus, error) {
	out := make([]CredentialStatus, 0, len(CredentialProviders))
	for _, p := range CredentialProviders {
		ok, err := s.Configured(p.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, CredentialStatus{Provider: p.ID, Label: p.Label, Configured: ok})
	}
	return out, nil
}

func knownProvider(provider string) bool {
	return slices.ContainsFunc(CredentialProviders, func(p struct{ ID, Label string }) bool { return p.ID == provider })
}

// Save stores or replaces the key for provider. Adapters read the store on every call, so the key
// is in effect without a restart. A provider the settings screen does not manage, an empty key and
// a key with whitespace inside or too long for the OS store are refused (ValidationError; field "api_key").
func (s *CredentialService) Save(provider, apiKey string) error {
	if !knownProvider(provider) {
		return &ValidationError{Field: "provider", Message: "未対応のプロバイダーです"}
	}
	apiKey, problem := credential.CleanAPIKey(apiKey)
	if problem != "" {
		return &ValidationError{Field: "api_key", Message: problem}
	}
	err := s.store.Set(provider, apiKey)
	if errors.Is(err, credential.ErrTooLarge) {
		return &ValidationError{Field: "api_key", Message: "API キーが長すぎます"}
	}
	return err
}

// Configured reports whether a key exists for provider.
func (s *CredentialService) Configured(provider string) (bool, error) {
	return s.store.Has(provider)
}

// Remove deletes the key for provider. Removing a missing key is not an error.
func (s *CredentialService) Remove(provider string) error {
	if !knownProvider(provider) {
		return &ValidationError{Field: "provider", Message: "未対応のプロバイダーです"}
	}
	err := s.store.Delete(provider)
	if errors.Is(err, credential.ErrNotFound) {
		return nil
	}
	return err
}
