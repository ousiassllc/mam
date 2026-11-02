package gentestrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/ousiassllc/mam/backend/internal/repository"
)

// The prompt match (#916) plans its rounds as queued results whose prompt is not known yet and writes each prompt when the round
// starts, so the rest of the generation test (progress, cancel, history) sees a match as a batch like any other.

// ContinueBatch reopens a batch that is over, adds results to it (they start queued) and adds the estimate of the new rounds to its
// estimated cost. A batch that is unknown or not over (another go-on got there first) is repository.ErrNotFound.
func (r *Repository) ContinueBatch(ctx context.Context, batchID string, results []Result, addCost float64) error {
	return r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := execOne(ctx, tx, `UPDATE gentest_batch SET finished_at = NULL, estimated_cost = estimated_cost + ? WHERE id = ? AND finished_at IS NOT NULL`, addCost, batchID); err != nil {
			return err
		}
		for _, res := range results {
			if err := insertResult(ctx, tx, batchID, res); err != nil {
				return err
			}
		}
		return nil
	})
}

// DeleteQueued removes the queued results of the batch (the rounds of a match that were not needed) and returns how many there were.
func (r *Repository) DeleteQueued(ctx context.Context, batchID string) (int, error) {
	n := 0
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM gentest_result WHERE batch_id = ? AND status = 'queued'`, batchID)
		if err != nil {
			return err
		}
		c, err := res.RowsAffected()
		n = int(c)
		return err
	})
	return n, err
}

// SetResultPrompt writes the prompt a round is made with (it is the prompt that is sent: a match has no negative prompt) and the
// params of the round, and puts the prompt into the prompt history. An unknown result is repository.ErrNotFound.
func (r *Repository) SetResultPrompt(ctx context.Context, id, prompt string, params json.RawMessage, at time.Time) error {
	return r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := execOne(ctx, tx, `UPDATE gentest_result SET prompt = ?, sent_prompt = ?, params = ? WHERE id = ?`, prompt, prompt, string(orJSON(params, "{}")), id); err != nil {
			return err
		}
		return usePrompt(ctx, tx, PromptKey{Prompt: prompt}, at)
	})
}

// SetResultParams replaces the params of a result. An unknown result is repository.ErrNotFound.
func (r *Repository) SetResultParams(ctx context.Context, id string, params json.RawMessage) error {
	return r.exec(ctx, `UPDATE gentest_result SET params = ? WHERE id = ?`, string(orJSON(params, "{}")), id)
}

// execOne runs a statement in tx that must change exactly the row asked for.
func execOne(ctx context.Context, tx *sql.Tx, query string, args ...any) error {
	res, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return repository.ErrNotFound
	}
	return nil
}
