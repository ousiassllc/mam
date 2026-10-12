package service

import (
	"errors"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/agent/jev"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/credential"
)

// MaxAPIKeyBytes bounds a pasted API key. It is within the strictest OS secure storage limit
// (Windows Credential Manager: 2560 bytes); real keys are a few hundred bytes.
const MaxAPIKeyBytes = 2560

const msgAPIKeyTooLong = "API キーが長すぎます"

// CredentialProviders are the providers whose API key the settings screen manages, in display
// order. The ID is the credential.Store key the adapters read.
var CredentialProviders = []struct{ ID, Label string }{
	{string(llm.ProviderAnthropic), "Anthropic（Claude）"},
	{string(llm.ProviderOpenAI), "OpenAI"},
	{string(llm.ProviderGoogle), "Google（Gemini / Imagen）"},
	{string(modeladapter.VendorFlux), "Black Forest Labs（Flux）"},
	{string(modeladapter.VendorKling), "Kling"},
	{string(modeladapter.VendorSeedance), "Seedance"},
	{string(modeladapter.VendorRunway), "Runway"},
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
	for _, p := range CredentialProviders {
		if p.ID == provider {
			return true
		}
	}
	return false
}

// Save stores or replaces the key for provider. Adapters read the store on every call, so the key
// is in effect without a restart. A provider the settings screen does not manage, an empty key and
// a key with whitespace inside, and a key the OS store refuses for its size are refused (ValidationError; field "api_key") and the old key stays.
func (s *CredentialService) Save(provider, apiKey string) error {
	if !knownProvider(provider) {
		return &ValidationError{Field: "provider", Message: "未対応のプロバイダーです"}
	}
	apiKey = strings.TrimSpace(apiKey)
	switch {
	case apiKey == "":
		return &ValidationError{Field: "api_key", Message: "API キーを入力してください"}
	case len(apiKey) > MaxAPIKeyBytes:
		return &ValidationError{Field: "api_key", Message: msgAPIKeyTooLong}
	case strings.ContainsAny(apiKey, " \t\r\n"):
		return &ValidationError{Field: "api_key", Message: "API キーに空白や改行は含められません"}
	}
	if err := s.store.Set(provider, apiKey); err != nil {
		if errors.Is(err, credential.ErrTooLarge) {
			return &ValidationError{Field: "api_key", Message: msgAPIKeyTooLong}
		}
		return err
	}
	return nil
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
