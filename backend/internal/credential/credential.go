// Package credential stores external model API keys in the OS secure storage
// (macOS Keychain / Windows Credential Manager / Linux Secret Service).
//
// Keys are never written to plain-text files, and no method returns a key to
// callers other than the adapters that need it: settings screens only learn
// whether a key is configured (Has), never its value.
package credential

import (
	"errors"
	"fmt"
	"strings"

	"github.com/zalando/go-keyring"
)

// ServiceName is the service under which entries are registered in the OS store.
const ServiceName = "mam"

// ErrNotFound is returned by Get and Delete when no key is stored for the provider.
var ErrNotFound = errors.New("credential: not found")

// ErrTooLarge is returned by Set when the OS secure storage refuses the key for its size
// (Windows Credential Manager: 2560 bytes; macOS Keychain: about 3000 bytes with service and account).
var ErrTooLarge = errors.New("credential: api key too large for the OS secure storage")

// Store saves and loads API keys per provider (e.g. "kling", "seedance", "anthropic").
type Store interface {
	// Set stores or replaces the key for provider, or returns ErrTooLarge if the OS store refuses its size.
	Set(provider, apiKey string) error
	// Get returns the key for provider, or ErrNotFound.
	Get(provider string) (string, error)
	// Delete removes the key for provider, or returns ErrNotFound.
	Delete(provider string) error
	// Has reports whether a key is stored for provider without exposing it.
	Has(provider string) (bool, error)
}

// Keyring is the Store backed by the OS secure storage. A call the OS store does not answer within
// DefaultTimeout (a Secret Service waiting for an unlock prompt) fails with ErrUnavailable.
type Keyring struct{ Store }

// NewKeyring returns the OS secure storage Store.
func NewKeyring() *Keyring { return &Keyring{Store: WithTimeout(osStore{}, DefaultTimeout)} }

var _ Store = (*Keyring)(nil)

// osStore calls go-keyring directly, with no time limit.
type osStore struct{}

func account(provider string) (string, error) {
	if strings.TrimSpace(provider) == "" {
		return "", errors.New("credential: provider must not be empty")
	}
	return "api-key:" + provider, nil
}

// Set implements Store.
func (osStore) Set(provider, apiKey string) error {
	acct, err := account(provider)
	if err != nil {
		return err
	}
	if apiKey == "" {
		return errors.New("credential: api key must not be empty")
	}
	if err := keyring.Set(ServiceName, acct, apiKey); err != nil {
		if errors.Is(err, keyring.ErrSetDataTooBig) {
			return fmt.Errorf("store api key for %q: %w", provider, ErrTooLarge)
		}
		return fmt.Errorf("store api key for %q: %w", provider, err)
	}
	return nil
}

// Get implements Store.
func (osStore) Get(provider string) (string, error) {
	acct, err := account(provider)
	if err != nil {
		return "", err
	}
	secret, err := keyring.Get(ServiceName, acct)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("load api key for %q: %w", provider, err)
	}
	return secret, nil
}

// Delete implements Store.
func (osStore) Delete(provider string) error {
	acct, err := account(provider)
	if err != nil {
		return err
	}
	err = keyring.Delete(ServiceName, acct)
	if errors.Is(err, keyring.ErrNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("delete api key for %q: %w", provider, err)
	}
	return nil
}

// Has implements Store.
func (k osStore) Has(provider string) (bool, error) {
	_, err := k.Get(provider)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}
