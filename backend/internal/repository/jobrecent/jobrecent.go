// Package jobrecent reads the jobs of the job table for the job list screen (spec/components/jobs-screen.md). It sits beside the
// per-aggregate repositories of the parent package, whose JobRepository writes the table and finds a job by its id or its status; the
// table is not pruned, so the screen needs a bounded read of its own. It only reads.
package jobrecent

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// Repository reads the job table.
type Repository struct{ db *repository.DB }

// New returns a Repository over db.
func New(db *repository.DB) *Repository { return &Repository{db: db} }

const query = `SELECT id, kind, payload, status, attempts, error, created_at, started_at, finished_at FROM job
	WHERE status IN ('queued', 'running') OR id IN (SELECT id FROM job ORDER BY ` + newestFirst + ` LIMIT ?)
	ORDER BY ` + newestFirst

// newestFirst is the order of the list: the newest start first; a job that has not started is where it was queued (spec/components/jobs-screen.md).
const newestFirst = `COALESCE(started_at, created_at) DESC, id DESC`

// ListRecent returns the newest `newest` jobs and, however old, every job that has not finished (queued or running), newest start first.
func (r *Repository) ListRecent(ctx context.Context, newest int) ([]domain.Job, error) {
	rows, err := r.db.Reader().QueryContext(ctx, query, newest)
	if err != nil {
		return nil, fmt.Errorf("list recent jobs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var jobs []domain.Job
	for rows.Next() {
		job, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("list recent jobs: %w", err)
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list recent jobs: %w", err)
	}
	return jobs, nil
}

func scan(rows *sql.Rows) (domain.Job, error) {
	var (
		job               domain.Job
		payload, created  string
		started, finished sql.NullString
	)
	if err := rows.Scan(&job.ID, &job.Kind, &payload, &job.Status, &job.Attempts, &job.Error, &created, &started, &finished); err != nil {
		return domain.Job{}, err
	}
	job.Payload = json.RawMessage(payload)
	var err error
	if job.CreatedAt, err = repository.ParseTime(created); err != nil {
		return domain.Job{}, fmt.Errorf("parse created_at: %w", err)
	}
	if job.StartedAt, err = nullTime(started); err != nil {
		return domain.Job{}, fmt.Errorf("parse started_at: %w", err)
	}
	if job.FinishedAt, err = nullTime(finished); err != nil {
		return domain.Job{}, fmt.Errorf("parse finished_at: %w", err)
	}
	return job, nil
}

func nullTime(s sql.NullString) (*time.Time, error) {
	if !s.Valid {
		return nil, nil
	}
	t, err := repository.ParseTime(s.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}
