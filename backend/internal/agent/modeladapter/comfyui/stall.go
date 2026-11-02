package comfyui

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

// A ComfyUI job is not cut off for taking long, only for showing no sign of progress for Client.Stall (the
// endpoint's timeout): a message of the job's own on the socket (a step of a node, a node starting, the cached
// nodes), so a job that keeps stepping runs as long as it needs. The call's own limit is the ceiling of
// localcfg.Endpoint.CallLimit, which only stops a runaway.
//
// Without a socket (the server has none, or it was lost) no sign arrives, and the limit runs from the last one:
// such a job is held to the timeout as a whole, as every call was before the limit followed progress.

// idle is how long the prompt has shown no sign of progress.
func (t *tracker) idle() time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.now().Sub(t.signed)
}

// touch counts now as a sign of progress.
func (t *tracker) touch() {
	t.mu.Lock()
	t.signed = t.now()
	t.mu.Unlock()
}

// stalled is the timeout error of a prompt that showed no sign of progress for the endpoint's limit, nil while it has
// (or the endpoint has no limit). A prompt that waits in the server's queue, behind another client's job, shows
// nothing, and is not stalled: it is looked for in the queue only once the limit has run out, so a healthy job costs
// no extra request.
func (b *Backend) stalled(ctx context.Context, promptID string, t *tracker) error {
	limit := b.Client.Stall
	if limit <= 0 || t.idle() < limit {
		return nil
	}
	if q, err := b.queue(ctx); err == nil && inQueue(q.Pending, promptID) {
		t.touch()
		return nil
	}
	return localcfg.MarkStall(limit, fmt.Errorf("%w after %s without progress: %w", modeladapter.ErrTimeout, limit, context.DeadlineExceeded))
}
