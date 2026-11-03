package jobqueue

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/apperr/errmap"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

func (q *Queue) fail(ctx context.Context, job domain.Job, reason string) {
	q.settleFailed(ctx, job, reason, "", "")
}

// failWith fails the job with err: the reason is its text, and the failure is kept as the common error (#1010) so that get_job and the
// screens can say the cause and the remedy in words. A failure nobody knows gets a correlation id, which the log line carries too.
func (q *Queue) failWith(ctx context.Context, job domain.Job, err error) {
	var e *apperr.Error
	if q.explain != nil {
		e = q.explain(err)
	}
	if e == nil {
		e = errmap.Explain(err)
	}
	if e.Code == apperr.CodeUnknown && e.ID == "" {
		e.ID = apperr.NewID()
	}
	q.settleFailed(ctx, job, err.Error(), e.Marshal(), e.ID)
}

func (q *Queue) settleFailed(ctx context.Context, job domain.Job, reason, info, errorID string) {
	attrs := []any{"job_id", job.ID, "kind", job.Kind, "reason", reason}
	if errorID != "" {
		attrs = append(attrs, "error_id", errorID)
	}
	q.logger.Error("job failed", attrs...)
	if err := q.repo.MarkFailedWithInfo(ctx, job.ID, reason, info); err != nil {
		q.logger.Error("mark job failed failed", "job_id", job.ID, "err", err)
	}
}
