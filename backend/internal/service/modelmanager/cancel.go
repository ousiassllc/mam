package modelmanager

import (
	"context"
	"errors"
)

// errCancelled is why an install the user stopped failed. The job is failed, not requeued: the user starts the
// install again, which continues from the .part files.
var errCancelled = errors.New("導入を中断しました。もう一度導入すると、ダウンロード済みの続きから再開します")

// cancellable returns ctx with a cancel that CancelInstall reaches by the id of the job, and what to call when the
// handler ends.
func (s *Service) cancellable(ctx context.Context, jobID string) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(ctx)
	s.mu.Lock()
	s.cancels[jobID] = cancel
	s.mu.Unlock()
	return ctx, func() {
		s.mu.Lock()
		delete(s.cancels, jobID)
		s.mu.Unlock()
		cancel(nil)
	}
}

// CancelInstall stops a running install (the engine's or a model's) by the id of its job. The job ends as failed
// with a message that says so; the files downloaded so far stay, and installing again resumes from them. A job
// that is not running (queued, ended, or not an install) is ErrNotFound.
func (s *Service) CancelInstall(jobID string) error {
	s.mu.Lock()
	cancel, ok := s.cancels[jobID]
	s.mu.Unlock()
	if !ok {
		return ErrNotFound
	}
	cancel(errCancelled)
	return nil
}
