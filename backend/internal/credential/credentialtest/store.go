// Package credentialtest gives tests a credential.Store of their own.
//
// The OS key store mock of go-keyring is one process-wide variable that every keyring.MockInit
// replaces, so tests that use it cannot run in parallel. A Memory is separate for each test.
package credentialtest

import (
	"errors"
	"strings"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/credential"
)

// Memory is a credential.Store that keeps the keys in memory.
type Memory struct {
	mu   sync.Mutex
	keys map[string]string
	err  error
	max  int
}

var _ credential.Store = (*Memory)(nil)

// New returns an empty Memory.
func New() *Memory { return &Memory{keys: map[string]string{}} }

// LimitBytes makes Set refuse a value longer than n bytes with credential.ErrTooLarge, as the Windows
// Credential Manager does at 2560. Zero lifts the limit.
func (m *Memory) LimitBytes(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.max = n
}

// FailWith makes every following call fail with err, as an OS key store that is not available does.
func (m *Memory) FailWith(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.err = err
}

func (m *Memory) check(provider string) error {
	if m.err != nil {
		return m.err
	}
	if strings.TrimSpace(provider) == "" {
		return errors.New("credential: provider must not be empty")
	}
	return nil
}

// Set implements credential.Store.
func (m *Memory) Set(provider, apiKey string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.check(provider); err != nil {
		return err
	}
	if apiKey == "" {
		return errors.New("credential: api key must not be empty")
	}
	if m.max > 0 && len(apiKey) > m.max {
		return credential.ErrTooLarge
	}
	m.keys[provider] = apiKey
	return nil
}

// Get implements credential.Store.
func (m *Memory) Get(provider string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.check(provider); err != nil {
		return "", err
	}
	key, ok := m.keys[provider]
	if !ok {
		return "", credential.ErrNotFound
	}
	return key, nil
}

// Delete implements credential.Store.
func (m *Memory) Delete(provider string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.check(provider); err != nil {
		return err
	}
	if _, ok := m.keys[provider]; !ok {
		return credential.ErrNotFound
	}
	delete(m.keys, provider)
	return nil
}

// Has implements credential.Store.
func (m *Memory) Has(provider string) (bool, error) {
	_, err := m.Get(provider)
	if errors.Is(err, credential.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}
