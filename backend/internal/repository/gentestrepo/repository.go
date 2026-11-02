package gentestrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// Repository persists the generation test history. All writes go through DB.Write.
type Repository struct {
	db  *repository.DB
	now func() time.Time
}

// New returns a Repository backed by db.
func New(db *repository.DB) *Repository { return &Repository{db: db, now: time.Now} }

func ts(t time.Time) string { return repository.FormatTime(t) }

// CreateBatch writes a batch with its inputs, its queued results and the prompts it used, in one transaction. A prompt
// that is in the history already counts one more use and moves to the top of the list.
func (r *Repository) CreateBatch(ctx context.Context, nb NewBatch) error {
	return r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		b := nb.Batch
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO gentest_batch (`+batchColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, NULL)`,
			b.ID, b.Media, b.JobID, string(orJSON(b.Params, "{}")), b.EstimatedCost, string(orJSON(b.Estimate, "{}")), ts(b.CreatedAt)); err != nil {
			return fmt.Errorf("insert gentest batch: %w", err)
		}
		for _, in := range nb.Inputs {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO gentest_input (`+inputColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				in.ID, b.ID, in.Role, in.FilePath, in.MimeType, in.SizeBytes, string(orJSON(in.Origin, "{}")), in.Position, in.Strength); err != nil {
				return fmt.Errorf("insert gentest input: %w", err)
			}
		}
		for _, res := range nb.Results {
			if err := insertResult(ctx, tx, b.ID, res); err != nil {
				return err
			}
		}
		for _, p := range nb.Prompts {
			if err := usePrompt(ctx, tx, p, b.CreatedAt); err != nil {
				return err
			}
		}
		return nil
	})
}

func insertResult(ctx context.Context, tx *sql.Tx, batchID string, r Result) error {
	used, err := json.Marshal(append([]string{}, r.UsedInputs...))
	if err != nil {
		return err
	}
	var seed any
	if r.Seed != nil {
		seed = *r.Seed
	}
	var est any
	if r.EstimatedCost != nil {
		est = *r.EstimatedCost
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO gentest_result (id, batch_id, position, media, model, api_model, prompt, negative_prompt, sent_prompt, negative_mode, params,
			used_inputs, seed, seed_sent, status, cost_basis, estimated_cost, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'queued', ?, ?, ?)`,
		r.ID, batchID, r.Position, r.Media, r.Model, r.APIModel, r.Prompt, r.NegativePrompt, r.SentPrompt, r.NegativeMode, string(orJSON(r.Params, "{}")),
		string(used), seed, boolInt(r.SeedSent), r.CostBasis, est, ts(r.CreatedAt))
	if err != nil {
		return fmt.Errorf("insert gentest result: %w", err)
	}
	return nil
}

func anySlice(in []string) []any {
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}

func orJSON(raw json.RawMessage, def string) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(def)
	}
	return raw
}

func usePrompt(ctx context.Context, tx *sql.Tx, p PromptKey, at time.Time) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO gentest_prompt (id, prompt, negative_prompt, use_count, created_at, last_used_at) VALUES (?, ?, ?, 1, ?, ?)
		 ON CONFLICT (prompt, negative_prompt) DO UPDATE SET use_count = use_count + 1, last_used_at = excluded.last_used_at`,
		domain.NewID(), p.Prompt, p.NegativePrompt, ts(at), ts(at))
	if err != nil {
		return fmt.Errorf("record gentest prompt: %w", err)
	}
	return nil
}

// SetBatchJob records the job that runs the batch. An unknown batch is repository.ErrNotFound.
func (r *Repository) SetBatchJob(ctx context.Context, batchID, jobID string) error {
	return r.exec(ctx, `UPDATE gentest_batch SET job_id = ? WHERE id = ?`, jobID, batchID)
}

// FinishBatch records when the batch ended.
func (r *Repository) FinishBatch(ctx context.Context, batchID string, at time.Time) error {
	return r.exec(ctx, `UPDATE gentest_batch SET finished_at = ? WHERE id = ? AND finished_at IS NULL`, ts(at), batchID)
}

// DeleteBatch removes the batch with its inputs, results and import records (the prompts stay). An unknown batch is
// repository.ErrNotFound.
func (r *Repository) DeleteBatch(ctx context.Context, batchID string) error {
	return r.exec(ctx, `DELETE FROM gentest_batch WHERE id = ?`, batchID)
}

// exec runs a statement that must change a row.
func (r *Repository) exec(ctx context.Context, query string, args ...any) error {
	return r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, query, args...)
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

// GetBatch returns the batch, with the number of its inputs and results, or repository.ErrNotFound.
func (r *Repository) GetBatch(ctx context.Context, id string) (Batch, error) {
	return r.batch(ctx, `WHERE id = ?`, id)
}

// BatchByJob returns the batch a job runs, or repository.ErrNotFound.
func (r *Repository) BatchByJob(ctx context.Context, jobID string) (Batch, error) {
	return r.batch(ctx, `WHERE job_id = ?`, jobID)
}

func (r *Repository) batch(ctx context.Context, where string, arg any) (Batch, error) {
	b, err := scanBatch(r.db.Reader().QueryRowContext(ctx, `SELECT `+batchColumns+` FROM gentest_batch `+where, arg))
	if err != nil {
		return Batch{}, notFound(err)
	}
	err = r.db.Reader().QueryRowContext(ctx,
		`SELECT (SELECT COUNT(*) FROM gentest_input WHERE batch_id = ?), COUNT(*), COALESCE(SUM(status = 'succeeded'), 0) FROM gentest_result WHERE batch_id = ?`, b.ID, b.ID).
		Scan(&b.InputCount, &b.ResultCount, &b.SucceededCount)
	return b, err
}

// Inputs returns the inputs of the batch in order.
func (r *Repository) Inputs(ctx context.Context, batchID string) ([]Input, error) {
	return queryAll(ctx, r, scanInput, `SELECT `+inputColumns+` FROM gentest_input WHERE batch_id = ? ORDER BY position, id`, batchID)
}

// GetInput returns one input, or repository.ErrNotFound.
func (r *Repository) GetInput(ctx context.Context, id string) (Input, error) {
	in, err := scanInput(r.db.Reader().QueryRowContext(ctx, `SELECT `+inputColumns+` FROM gentest_input WHERE id = ?`, id))
	return in, notFound(err)
}

func queryAll[T any](ctx context.Context, r *Repository, scan func(scanner) (T, error), query string, args ...any) ([]T, error) {
	rows, err := r.db.Reader().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []T{}
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// Results returns the results of the batch in the order they were planned.
func (r *Repository) Results(ctx context.Context, batchID string) ([]Result, error) {
	return queryAll(ctx, r, scanResult, `SELECT `+resultColumns+` FROM gentest_result WHERE batch_id = ? ORDER BY position, id`, batchID)
}

// GetResult returns the result, or repository.ErrNotFound.
func (r *Repository) GetResult(ctx context.Context, id string) (Result, error) {
	res, err := scanResult(r.db.Reader().QueryRowContext(ctx, `SELECT `+resultColumns+` FROM gentest_result WHERE id = ?`, id))
	return res, notFound(err)
}

// ListResults returns the results matching the filter, newest first.
func (r *Repository) ListResults(ctx context.Context, f Filter) ([]Result, error) {
	var where []string
	var args []any
	add := func(cond string, arg ...any) { where = append(where, cond); args = append(args, arg...) }
	if f.Model != "" {
		add(`model = ?`, f.Model)
	}
	if len(f.Models) > 0 {
		add(`model IN (`+strings.TrimSuffix(strings.Repeat("?,", len(f.Models)), ",")+`)`, anySlice(f.Models)...)
	}
	if f.Media != "" {
		add(`media = ?`, f.Media)
	}
	if f.FavoriteOnly {
		add(`favorite = 1`)
	}
	if f.BatchID != "" {
		add(`batch_id = ?`, f.BatchID)
	}
	if f.Status != "" {
		add(`status = ?`, f.Status)
	}
	if f.SucceededOnly {
		add(`status = 'succeeded' AND file_path <> ''`)
	}
	q := `SELECT ` + resultColumns + ` FROM gentest_result`
	if len(where) > 0 {
		q += ` WHERE ` + strings.Join(where, ` AND `)
	}
	q += ` ORDER BY created_at DESC, batch_id DESC, position, id`
	if f.Limit > 0 {
		q += fmt.Sprintf(` LIMIT %d OFFSET %d`, f.Limit, max(f.Offset, 0))
	}
	return queryAll(ctx, r, scanResult, q, args...) //nolint:gosec // q is assembled from constants; only the arguments are data
}

// Models returns the models the history has results of, for the filter.
func (r *Repository) Models(ctx context.Context) ([]string, error) {
	return queryAll(ctx, r, func(s scanner) (string, error) {
		var m string
		return m, s.Scan(&m)
	}, `SELECT DISTINCT model FROM gentest_result ORDER BY model`)
}

// SetFavorite sets or clears the favorite mark of a result. An unknown result is repository.ErrNotFound.
func (r *Repository) SetFavorite(ctx context.Context, id string, favorite bool) error {
	return r.exec(ctx, `UPDATE gentest_result SET favorite = ? WHERE id = ?`, boolInt(favorite), id)
}

// DeleteResult removes the result and its import records and returns how many results the batch has left (a batch
// without results is removed too, with its inputs). An unknown result is repository.ErrNotFound.
func (r *Repository) DeleteResult(ctx context.Context, id string) (batchID string, left int, err error) {
	err = r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT batch_id FROM gentest_result WHERE id = ?`, id).Scan(&batchID); err != nil {
			return notFound(err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM gentest_result WHERE id = ?`, id); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM gentest_result WHERE batch_id = ?`, batchID).Scan(&left); err != nil {
			return err
		}
		if left == 0 {
			_, err := tx.ExecContext(ctx, `DELETE FROM gentest_batch WHERE id = ?`, batchID)
			return err
		}
		return nil
	})
	return batchID, left, err
}

// Counts returns the results of the batch by status.
func (r *Repository) Counts(ctx context.Context, batchID string) (Counts, error) {
	var c Counts
	err := r.db.Reader().QueryRowContext(ctx,
		`SELECT COALESCE(SUM(status = 'queued'), 0), COALESCE(SUM(status = 'running'), 0), COALESCE(SUM(status = 'succeeded'), 0),
			COALESCE(SUM(status = 'failed'), 0), COALESCE(SUM(status = 'cancelled'), 0) FROM gentest_result WHERE batch_id = ?`, batchID).
		Scan(&c.Queued, &c.Running, &c.Succeeded, &c.Failed, &c.Cancelled)
	return c, err
}

// Spend is the total of the history page: the results that made a file.
func (r *Repository) Spend(ctx context.Context) (Spend, error) {
	var s Spend
	err := r.db.Reader().QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(COALESCE(actual_cost, estimated_cost, 0)), 0), COALESCE(SUM(cost_basis = 'unknown' AND actual_cost IS NULL), 0)
		 FROM gentest_result WHERE status = 'succeeded'`).Scan(&s.Items, &s.PricedUSD, &s.UnknownItems)
	return s, err
}
