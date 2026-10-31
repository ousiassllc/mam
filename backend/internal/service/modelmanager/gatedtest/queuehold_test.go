package gatedtest_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
)

// queueWorkers is the number of workers of the rig's job queue (mmtest.New).
const queueWorkers = 2

// queueHold keeps every worker of the rig's job queue busy, so that a job queued meanwhile stays queued until the hold is released:
// the test can change what Hugging Face answers after the check and before the job asks, without a sleep. It borrows the handler of
// a job kind the rig does not use, so it MUST be made before the queue is started (the engine install starts it).
type queueHold struct {
	mu      sync.Mutex
	gate    chan struct{}
	started chan struct{}
}

func newQueueHold(r *mmtest.Rig) *queueHold {
	h := &queueHold{started: make(chan struct{}, queueWorkers)}
	r.Queue.Register(domain.JobRepair, h.handle, false)
	return h
}

func (h *queueHold) handle(ctx context.Context, _ domain.Job) error {
	h.mu.Lock()
	gate := h.gate
	h.mu.Unlock()
	h.started <- struct{}{}
	select {
	case <-gate:
	case <-ctx.Done():
	}
	return nil
}

// Hold returns once every worker is held; release lets them go (safe to call more than once).
func (h *queueHold) Hold(t *testing.T, r *mmtest.Rig) (release func()) {
	t.Helper()
	gate := make(chan struct{})
	h.mu.Lock()
	h.gate = gate
	h.mu.Unlock()
	var once sync.Once
	release = func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)
	for range queueWorkers {
		_, err := r.Queue.Enqueue(context.Background(), domain.JobRepair, struct{}{})
		require.NoError(t, err)
	}
	for range queueWorkers {
		select {
		case <-h.started:
		case <-time.After(30 * time.Second):
			require.FailNow(t, "the workers of the queue were not all held")
		}
	}
	return release
}
