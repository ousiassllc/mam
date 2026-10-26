package chatgptsettings_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/mam/backend/internal/chatgpt"
)

// gate holds every wait of the chatgpt service (the device login's poll interval) until open is called, so a test sees
// the login while it is still pending instead of racing the poll loop that would finish it first.
type gate struct {
	once sync.Once
	ch   chan struct{}
}

// newGate returns a closed-until-opened gate; it opens at the end of the test so no wait outlives it.
func newGate(t *testing.T) *gate {
	t.Helper()
	g := &gate{ch: make(chan struct{})}
	t.Cleanup(g.open)
	return g
}

func (g *gate) open() { g.once.Do(func() { close(g.ch) }) }

// tweak makes the service wait on the gate (or on ctx, which ends a login that is cancelled).
func (g *gate) tweak(o *chatgpt.Options) {
	o.Sleep = func(ctx context.Context, _ time.Duration) error {
		select {
		case <-g.ch:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
