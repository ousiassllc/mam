package credentialtest

import (
	"errors"
	"sync"
	"testing"

	"github.com/ousiassllc/mam/backend/internal/credential"
)

// Blocking is a credential.Store whose every call hangs until the test ends, as the Linux Secret
// Service does while it waits for an unlock prompt nobody answers.
type Blocking struct {
	release chan struct{}
	once    sync.Once
}

var _ credential.Store = (*Blocking)(nil)

// NewBlocking returns a Blocking that lets the hung calls go when the test ends.
func NewBlocking(t testing.TB) *Blocking {
	t.Helper()
	b := &Blocking{release: make(chan struct{})}
	t.Cleanup(func() { b.once.Do(func() { close(b.release) }) })
	return b
}

var errReleased = errors.New("credentialtest: blocking store released")

// Set implements credential.Store.
func (b *Blocking) Set(string, string) error {
	<-b.release
	return errReleased
}

// Get implements credential.Store.
func (b *Blocking) Get(string) (string, error) {
	<-b.release
	return "", errReleased
}

// Delete implements credential.Store.
func (b *Blocking) Delete(string) error {
	<-b.release
	return errReleased
}

// Has implements credential.Store.
func (b *Blocking) Has(string) (bool, error) {
	<-b.release
	return false, errReleased
}
