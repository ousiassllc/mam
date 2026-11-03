// Package recoverwork is what the app does at start-up about the work an abnormal termination left behind. It is a package of its own, under app,
// so that app stays within its size (linterly).
package recoverwork

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// Run cleans up after an abnormal termination: Shots left GENERATING / REPAIRING are released
// before their jobs are failed, the Shots whose job the queue then failed (a judge job out of attempts,
// a job without handler) are released after it, and the editing follow-ups that the stop left undone are
// woken.
func Run(ctx context.Context, services *service.Services, jobs *jobqueue.Queue, logger *slog.Logger) error {
	released, err := services.Recover(ctx)
	if err != nil {
		return fmt.Errorf("recover shots: %w", err)
	}
	if released > 0 {
		logger.Warn("released shots interrupted while generating", "count", released)
	}
	resumed, failed, err := jobs.Recover(ctx)
	if err != nil {
		return fmt.Errorf("recover jobs: %w", err)
	}
	// A job the queue failed will never run its handler, which is what releases its Shot.
	released, err = services.Recover(ctx)
	if err != nil {
		return fmt.Errorf("recover shots of failed jobs: %w", err)
	}
	if released > 0 {
		logger.Warn("released shots whose job failed at recovery", "count", released)
	}
	if resumed+failed > 0 {
		logger.Warn("recovered unfinished jobs after abnormal termination", "resumed", resumed, "failed", failed)
	}
	// A Reshoot round that was ready, or an AUTO Project that waits for its editing stage, when the
	// process stopped has no job change or Take selection left to wake the worker.
	services.Editor.ResumeEditing()
	return nil
}
