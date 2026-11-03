package voicetraintest_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
	"github.com/ousiassllc/mam/backend/internal/service/voicetrain"
	"github.com/ousiassllc/mam/backend/internal/worktrack"
)

// gated holds Start inside BeginTool (the training is published as the one that runs, but its goroutine is not started yet) and returns what Start
// answers once release is called.
func gated(t *testing.T, f *fixture) (release func(), result <-chan error) {
	t.Helper()
	f.rt.gate = make(chan struct{})
	f.rt.entered = make(chan struct{}, 1)
	res := make(chan error, 1)
	go func() {
		_, err := f.svc.Start(context.Background(), f.project, "c1", voicetrain.Options{})
		res <- err
	}()
	select {
	case <-f.rt.entered:
	case <-time.After(30 * time.Second):
		t.Fatal("Start did not reach the runtime")
	}
	return func() { close(f.rt.gate) }, res
}

func TestCancel_WhileStartIsStillAskingTheRuntimeEndsWhenStartRefuses(t *testing.T) {
	f := newFixture(t, "ok")
	f.ready()
	f.rt.refuse = localruntime.ErrNotInstalled
	release, started := gated(t, f)

	cancelled := make(chan error, 1)
	go func() { cancelled <- f.svc.Cancel(f.project, "c1") }() // used to be a nil function
	require.Never(t, func() bool { return len(cancelled) > 0 }, 100*time.Millisecond, 5*time.Millisecond, "Cancel waits for the end of the training")

	release()

	require.Error(t, <-started, "Start refuses as the runtime is not installed")
	select {
	case err := <-cancelled:
		assert.NoError(t, err, "a refused Start closes the run, so Cancel does not wait a minute")
	case <-time.After(30 * time.Second):
		t.Fatal("Cancel did not return")
	}
	assert.False(t, f.svc.Busy())
	assert.Equal(t, f.rt.held.Load(), f.rt.ended.Load())
}

func TestClose_WhileStartIsStillAskingTheRuntimeCancelsTheTraining(t *testing.T) {
	f := newFixture(t, "ok")
	f.ready()
	release, started := gated(t, f)

	f.svc.Close() // used to be a nil function

	release()
	require.NoError(t, <-started)
	f.wait(f.svc.Works()[0].ID, worktrack.Cancelled)
	f.finished()
	assert.Zero(t, f.rt.specs.Load(), "a cancelled training starts no process")
}
