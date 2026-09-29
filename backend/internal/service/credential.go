package service

import (
	"errors"

	"github.com/ousiassllc/mam/backend/internal/credential"
)

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

// Save stores or replaces the key for provider.
func (s *CredentialService) Save(provider, apiKey string) error {
	return s.store.Set(provider, apiKey)
}

// Configured reports whether a key exists for provider.
func (s *CredentialService) Configured(provider string) (bool, error) {
	return s.store.Has(provider)
}

// Remove deletes the key for provider. Removing a missing key is not an error.
func (s *CredentialService) Remove(provider string) error {
	err := s.store.Delete(provider)
	if errors.Is(err, credential.ErrNotFound) {
		return nil
	}
	return err
}
