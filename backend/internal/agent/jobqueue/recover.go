package jobqueue

import (
	"context"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Recover resolves jobs left in the running state by an abnormal termination:
// resumable ones (below MaxAttempts) are queued again, all others are failed. A queued job whose kind
// has no handler is failed too (it would be when a worker claimed it), so that the work its Shot waits
// for is known to be over before Start. It returns the number of resumed and failed jobs. Call it
// before Start.
func (q *Queue) Recover(ctx context.Context) (resumed, failed int, err error) {
	jobs, err := q.repo.ListByStatus(ctx, domain.JobRunning)
	if err != nil {
		return 0, 0, err
	}
	for _, job := range jobs {
		q.mu.Lock()
		reg, ok := q.handlers[job.Kind]
		q.mu.Unlock()

		switch {
		case ok && reg.resumable && job.Attempts < q.maxAttempts:
			if err := q.repo.Requeue(ctx, job.ID); err != nil {
				return resumed, failed, err
			}
			resumed++
			q.logger.Warn("resuming interrupted job", "job_id", job.ID, "kind", job.Kind, "attempts", job.Attempts)
		default:
			if err := q.repo.MarkFailed(ctx, job.ID, InterruptedReason); err != nil {
				return resumed, failed, err
			}
			failed++
			q.logger.Warn("failed interrupted job", "job_id", job.ID, "kind", job.Kind, "attempts", job.Attempts)
		}
	}
	queued, err := q.repo.ListByStatus(ctx, domain.JobQueued)
	if err != nil {
		return resumed, failed, err
	}
	for _, job := range queued {
		q.mu.Lock()
		_, ok := q.handlers[job.Kind]
		q.mu.Unlock()
		if ok {
			continue
		}
		if err := q.repo.MarkFailed(ctx, job.ID, fmt.Sprintf("no handler registered for %q", job.Kind)); err != nil {
			return resumed, failed, err
		}
		failed++
		q.logger.Warn("failed queued job without handler", "job_id", job.ID, "kind", job.Kind)
	}
	return resumed, failed, nil
}
