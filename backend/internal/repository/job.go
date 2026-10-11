package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// ErrNotFound is returned when a requested row does not exist.
var ErrNotFound = errors.New("repository: not found")

const timeLayout = "2006-01-02T15:04:05.000Z"

// FormatTime writes a timestamp in the format the tables store (ParseTime reads it back).
func FormatTime(t time.Time) string { return t.UTC().Format(timeLayout) }

// JobRepository persists background jobs. All writes go through DB.Write.
type JobRepository struct {
	db  *DB
	now func() time.Time
}

// NewJobRepository returns a JobRepository backed by db.
func NewJobRepository(db *DB) *JobRepository {
	return &JobRepository{db: db, now: time.Now}
}

// Create inserts a queued job and returns it.
func (r *JobRepository) Create(ctx context.Context, kind domain.JobKind, payload json.RawMessage) (domain.Job, error) {
	if len(payload) == 0 {
		payload = json.RawMessage("{}")
	}
	job := domain.Job{
		ID:        domain.NewID(),
		Kind:      kind,
		Payload:   payload,
		Status:    domain.JobQueued,
		CreatedAt: r.now().UTC(),
	}
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO job (id, kind, payload, status, created_at) VALUES (?, ?, ?, ?, ?)`,
			job.ID, job.Kind, string(job.Payload), job.Status, FormatTime(job.CreatedAt))
		return err
	})
	if err != nil {
		return domain.Job{}, fmt.Errorf("create job: %w", err)
	}
	return job, nil
}

// Get returns the job with the given id or ErrNotFound.
func (r *JobRepository) Get(ctx context.Context, id string) (domain.Job, error) {
	row := r.db.Reader().QueryRowContext(ctx, jobSelect+` WHERE id = ?`, id)
	job, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Job{}, ErrNotFound
	}
	if err != nil {
		return domain.Job{}, fmt.Errorf("get job: %w", err)
	}
	return job, nil
}

// ListByIDs returns the jobs with the given ids keyed by id in one query. Ids without a job are
// absent from the result.
func (r *JobRepository) ListByIDs(ctx context.Context, ids []string) (map[string]domain.Job, error) {
	jobs := make(map[string]domain.Job, len(ids))
	if len(ids) == 0 {
		return jobs, nil
	}
	rows, err := r.db.Reader().QueryContext(ctx,
		jobSelect+` WHERE id IN (`+placeholders(len(ids))+`)`, toAny(ids)...)
	if err != nil {
		return nil, fmt.Errorf("list jobs by ids: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, fmt.Errorf("scan job: %w", err)
		}
		jobs[job.ID] = job
	}
	return jobs, rows.Err()
}

// ListByStatus returns jobs in the given status, oldest first.
func (r *JobRepository) ListByStatus(ctx context.Context, status domain.JobStatus) ([]domain.Job, error) {
	rows, err := r.db.Reader().QueryContext(ctx, jobSelect+` WHERE status = ? ORDER BY created_at, id`, status)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var jobs []domain.Job
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, fmt.Errorf("scan job: %w", err)
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

// ClaimNext atomically moves the oldest queued job to running (incrementing attempts)
// and returns it. It returns ErrNotFound when no job is queued.
func (r *JobRepository) ClaimNext(ctx context.Context) (domain.Job, error) {
	var job domain.Job
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, jobSelect+` WHERE status = 'queued' ORDER BY created_at, id LIMIT 1`)
		j, err := scanJob(row)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		started := r.now().UTC()
		if _, err := tx.ExecContext(ctx,
			`UPDATE job SET status = 'running', attempts = attempts + 1, started_at = ?, finished_at = NULL, error = '' WHERE id = ?`,
			FormatTime(started), j.ID); err != nil {
			return err
		}
		j.Status = domain.JobRunning
		j.Attempts++
		j.StartedAt = &started
		j.FinishedAt = nil
		j.Error = ""
		job = j
		return nil
	})
	if errors.Is(err, ErrNotFound) {
		return domain.Job{}, ErrNotFound
	}
	if err != nil {
		return domain.Job{}, fmt.Errorf("claim job: %w", err)
	}
	return job, nil
}

// MarkRunning moves a queued job to running and increments attempts.
// It returns ErrNotFound if the job is not queued (e.g. already claimed).
func (r *JobRepository) MarkRunning(ctx context.Context, id string) error {
	return r.transition(ctx, id,
		`UPDATE job SET status = 'running', attempts = attempts + 1, started_at = ?, finished_at = NULL, error = ''
		 WHERE id = ? AND status = 'queued'`, FormatTime(r.now()), id)
}

// MarkSucceeded moves a running job to succeeded.
func (r *JobRepository) MarkSucceeded(ctx context.Context, id string) error {
	return r.transition(ctx, id,
		`UPDATE job SET status = 'succeeded', finished_at = ? WHERE id = ? AND status = 'running'`,
		FormatTime(r.now()), id)
}

// MarkFailed moves a queued or running job to failed and records the reason.
func (r *JobRepository) MarkFailed(ctx context.Context, id, reason string) error {
	return r.transition(ctx, id,
		`UPDATE job SET status = 'failed', error = ?, finished_at = ? WHERE id = ? AND status IN ('queued', 'running')`,
		reason, FormatTime(r.now()), id)
}

// Requeue moves a running job back to queued so it is executed again.
func (r *JobRepository) Requeue(ctx context.Context, id string) error {
	return r.transition(ctx, id,
		`UPDATE job SET status = 'queued', finished_at = NULL WHERE id = ? AND status = 'running'`, id)
}

func (r *JobRepository) transition(ctx context.Context, id, query string, args ...any) error {
	return r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			return fmt.Errorf("job %s: %w", id, ErrNotFound)
		}
		return nil
	})
}

const jobSelect = `SELECT id, kind, payload, status, attempts, error, created_at, started_at, finished_at FROM job`

type rowScanner interface{ Scan(dest ...any) error }

func scanJob(s rowScanner) (domain.Job, error) {
	var (
		job               domain.Job
		payload, created  string
		started, finished sql.NullString
	)
	if err := s.Scan(&job.ID, &job.Kind, &payload, &job.Status, &job.Attempts, &job.Error,
		&created, &started, &finished); err != nil {
		return domain.Job{}, err
	}
	job.Payload = json.RawMessage(payload)
	var err error
	if job.CreatedAt, err = time.Parse(timeLayout, created); err != nil {
		return domain.Job{}, fmt.Errorf("parse created_at: %w", err)
	}
	if job.StartedAt, err = parseNullTime(started); err != nil {
		return domain.Job{}, fmt.Errorf("parse started_at: %w", err)
	}
	if job.FinishedAt, err = parseNullTime(finished); err != nil {
		return domain.Job{}, fmt.Errorf("parse finished_at: %w", err)
	}
	return job, nil
}

func parseNullTime(s sql.NullString) (*time.Time, error) {
	if !s.Valid {
		return nil, nil
	}
	t, err := time.Parse(timeLayout, s.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// placeholders returns n comma-separated "?" for an IN list.
func placeholders(n int) string { return strings.TrimSuffix(strings.Repeat("?,", n), ",") }

func toAny(ids []string) []any {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return args
}
