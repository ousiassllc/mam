package router_test

import (
	"errors"
	"strings"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/credential"
)

// memStore is the credential.Store of one test app. The OS key store mock of go-keyring is one
// process-wide variable that every MockInit replaces, so tests that share it cannot run in parallel;
// each app has a store of its own instead.
type memStore struct {
	mu   sync.Mutex
	keys map[string]string
	err  error
}

var _ credential.Store = (*memStore)(nil)

func newMemStore() *memStore { return &memStore{keys: map[string]string{}} }

// failWith makes every following call fail with err, as an OS key store that is not available does.
func (m *memStore) failWith(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.err = err
}

func (m *memStore) check(provider string) error {
	if m.err != nil {
		return m.err
	}
	if strings.TrimSpace(provider) == "" {
		return errors.New("credential: provider must not be empty")
	}
	return nil
}

func (m *memStore) Set(provider, apiKey string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.check(provider); err != nil {
		return err
	}
	if apiKey == "" {
		return errors.New("credential: api key must not be empty")
	}
	m.keys[provider] = apiKey
	return nil
}

func (m *memStore) Get(provider string) (string, error) {
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

func (m *memStore) Delete(provider string) error {
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

func (m *memStore) Has(provider string) (bool, error) {
	_, err := m.Get(provider)
	if errors.Is(err, credential.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}
