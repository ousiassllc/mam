package gentestrepo

import (
	"context"
	"database/sql"
	"time"

	"github.com/ousiassllc/mam/backend/internal/repository"
)

// MarkRunning moves a queued result to running and reports whether it did: a result that was cancelled meanwhile stays
// cancelled and must not be started.
func (r *Repository) MarkRunning(ctx context.Context, id string) (bool, error) {
	started := false
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE gentest_result SET status = 'running' WHERE id = ? AND status = 'queued'`, id)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		started = n == 1
		return err
	})
	return started, err
}

// Settle records how a result ended. Only a queued or running result is settled: one that is over already keeps its state.
func (r *Repository) Settle(ctx context.Context, id string, s Settlement) error {
	var actual any
	if s.ActualCost != nil {
		actual = *s.ActualCost
	}
	return r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE gentest_result SET status = ?, error = ?, file_path = ?, mime_type = ?, size_bytes = ?, actual_cost = ?, finished_at = ?
			 WHERE id = ? AND status IN ('queued', 'running')`,
			s.Status, s.Error, s.FilePath, s.MimeType, s.SizeBytes, actual, ts(s.At), id)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			return repository.ErrNotFound
		}
		return nil
	})
}

// CancelQueued marks every queued result of the batch cancelled with the reason and returns how many there were. Running
// results are not touched: their vendor call finishes and settles them.
func (r *Repository) CancelQueued(ctx context.Context, batchID, reason string, at time.Time) (int, error) {
	n := 0
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE gentest_result SET status = 'cancelled', error = ?, finished_at = ? WHERE batch_id = ? AND status = 'queued'`, reason, ts(at), batchID)
		if err != nil {
			return err
		}
		c, err := res.RowsAffected()
		n = int(c)
		return err
	})
	return n, err
}

// FailUnfinished fails every queued or running result of every batch with the reason and ends the batches that were not
// over. It runs at startup, after the job queue failed the interrupted jobs: a generation is paid for, so nothing resumes.
// Results that succeeded stay. It returns the number of results it failed.
func (r *Repository) FailUnfinished(ctx context.Context, reason string, at time.Time) (int, error) {
	n := 0
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE gentest_result SET status = 'failed', error = ?, finished_at = ? WHERE status IN ('queued', 'running')`, reason, ts(at))
		if err != nil {
			return err
		}
		c, err := res.RowsAffected()
		if err != nil {
			return err
		}
		n = int(c)
		_, err = tx.ExecContext(ctx, `UPDATE gentest_batch SET finished_at = ? WHERE finished_at IS NULL`, ts(at))
		return err
	})
	return n, err
}

// FailQueued fails the queued results of one batch with the reason (a batch whose job could not start). Running results are
// not touched: their goroutines settle them.
func (r *Repository) FailQueued(ctx context.Context, batchID, reason string, at time.Time) (int, error) {
	n := 0
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE gentest_result SET status = 'failed', error = ?, finished_at = ? WHERE batch_id = ? AND status = 'queued'`, reason, ts(at), batchID)
		if err != nil {
			return err
		}
		c, err := res.RowsAffected()
		n = int(c)
		return err
	})
	return n, err
}
