package llamaserver_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/service/llamaserver"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
)

// loadingRuntime is a runtime whose server takes a long time to load its model, as proc.Manager does: the first Prepare starts it and waits for the
// load (the context it was given ends the start and kills the server), a Prepare during the load joins it.
type loadingRuntime struct {
	loaded  chan struct{} // closed by the test: the model is loaded
	loading chan struct{} // closed when the server began to load

	began sync.Once

	mu       sync.Mutex
	starts   int
	killed   bool
	running  bool
	starting chan struct{}
}

func newLoadingRuntime() *loadingRuntime {
	return &loadingRuntime{loaded: make(chan struct{}), loading: make(chan struct{})}
}

func (r *loadingRuntime) Prepare(ctx context.Context, _ string) error {
	for {
		r.mu.Lock()
		switch {
		case r.running:
			r.mu.Unlock()
			return nil
		case r.starting != nil:
			wait := r.starting
			r.mu.Unlock()
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		r.starting = make(chan struct{})
		r.starts++
		r.mu.Unlock()
		return r.load(ctx)
	}
}

func (r *loadingRuntime) load(ctx context.Context) error {
	r.began.Do(func() { close(r.loading) })
	defer func() {
		r.mu.Lock()
		close(r.starting)
		r.starting = nil
		r.mu.Unlock()
	}()
	select {
	case <-r.loaded:
		r.mu.Lock()
		r.running = true
		r.mu.Unlock()
		return nil
	case <-ctx.Done():
		r.mu.Lock()
		r.killed = true
		r.mu.Unlock()
		return ctx.Err()
	}
}

func (r *loadingRuntime) Status(context.Context, string) (localruntime.View, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var v localruntime.View
	if r.running {
		v.Process = proc.Status{State: proc.StateRunning, URL: "http://127.0.0.1:18080"}
	}
	return v, nil
}

func (r *loadingRuntime) Token(string) (string, error) { return "start-key", nil }
func (r *loadingRuntime) Stop(string) error            { return nil }

func (r *loadingRuntime) snapshot() (starts int, killed bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.starts, r.killed
}

// A call whose context ends while the model loads (the timeout of one call, 120 seconds, is shorter than a big model's load) gives up, but the start goes
// on: the server is not killed, and the next call joins that start instead of loading the model again.
func TestAServerLoadingItsModelIsNotKilledWhenTheCallEnds(t *testing.T) {
	rt := newLoadingRuntime()
	server := llamaserver.Server{Runtime: rt}

	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		_, _, err := server.Endpoint(ctx)
		errc <- err
	}()
	waitClosed(t, rt.loading)
	cancel()
	select {
	case err := <-errc:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(10 * time.Second):
		t.Fatal("the call did not give up when its context ended")
	}

	next := make(chan error, 1)
	var url, key string
	go func() {
		var err error
		url, key, err = server.Endpoint(context.Background())
		next <- err
	}()
	close(rt.loaded)
	select {
	case err := <-next:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("the next call did not join the start in progress")
	}
	assert.Equal(t, "http://127.0.0.1:18080", url)
	assert.Equal(t, "start-key", key)
	starts, killed := rt.snapshot()
	assert.Equal(t, 1, starts, "one load for the abandoned call and the next one")
	assert.False(t, killed, "the half-loaded server was not killed")
}

// Calls that arrive together start the server once.
func TestCallsTogetherStartTheServerOnce(t *testing.T) {
	rt := newLoadingRuntime()
	server := llamaserver.Server{Runtime: rt}

	const calls = 4
	errs := make(chan error, calls)
	for range calls {
		go func() {
			_, _, err := server.Endpoint(context.Background())
			errs <- err
		}()
	}
	waitClosed(t, rt.loading)
	close(rt.loaded)
	for range calls {
		select {
		case err := <-errs:
			require.NoError(t, err)
		case <-time.After(10 * time.Second):
			t.Fatal("a call did not return")
		}
	}
	starts, killed := rt.snapshot()
	assert.Equal(t, 1, starts)
	assert.False(t, killed)
}

// A start that fails is the error of the call, as before.
func TestAFailedStartIsTheErrorOfTheCall(t *testing.T) {
	server := llamaserver.Server{Runtime: failingRuntime{err: errors.New("boom")}}
	_, _, err := server.Endpoint(context.Background())
	var unavailable *llamaserver.UnavailableError
	require.ErrorAs(t, err, &unavailable)
}

type failingRuntime struct{ err error }

func (f failingRuntime) Prepare(context.Context, string) error { return f.err }
func (f failingRuntime) Status(context.Context, string) (localruntime.View, error) {
	return localruntime.View{}, nil
}
func (f failingRuntime) Token(string) (string, error) { return "", nil }
func (f failingRuntime) Stop(string) error            { return nil }

func waitClosed(t *testing.T, c <-chan struct{}) {
	t.Helper()
	select {
	case <-c:
	case <-time.After(10 * time.Second):
		t.Fatal("the server did not begin to load")
	}
}
