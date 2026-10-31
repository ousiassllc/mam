// Package jobhistory is the repository of the history of the long operations the MCP server started (table
// mcp_job_history). It sits beside the per-aggregate repositories of the parent package and uses its DB and
// its error values. See spec/architecture/mcp.md §6 and er-mcp.md.
//
// The history is bounded: a row older than Retention is deleted, and so are the oldest rows beyond MaxRows.
// Both limits are enforced whenever a row is added (the same transaction) and by Prune, which the app runs
// at startup.
package jobhistory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/mam/backend/internal/repository"
)

// The limits of the history. 30 days is the retention of the log files (logging.maxAgeDays), so that a job
// and the log lines about it expire together. 10,000 rows is about 2.4 times the 4096 entries of the
// in-memory registry this table replaced: it covers 30 days at a few hundred operations a day, and at about
// 0.5 KB a row the table stays under 5 MB.
const (
	Retention = 30 * 24 * time.Hour
	MaxRows   = 10000
)

// Entry is one row of mcp_job_history.
type Entry struct {
	ID         string // the job_id
	Kind       string
	Target     string
	ProjectID  string // "" when the target belongs to no Project; Insert looks it up when it is empty
	QueueJobID string // "" when the service queued no job row
	StartedAt  time.Time

	State        string // queued | running | succeeded | failed | cancelled
	Message      string
	Done, Total  int
	Result       json.RawMessage // a JSON object, nil when there is none
	ErrorCode    string
	ErrorMessage string
	ErrorID      string // the correlation id of an internal failure
	FinishedAt   *time.Time
	EndedAt      *time.Time
}

// Open reports whether the job is not over (it may still change).
func (e Entry) Open() bool { return e.State == "queued" || e.State == "running" }

// Repository persists the history. All writes go through DB.Write.
type Repository struct {
	db        *repository.DB
	now       func() time.Time
	retention time.Duration
	maxRows   int
}

// New returns a Repository backed by db with the Retention and MaxRows limits.
func New(db *repository.DB) *Repository {
	return &Repository{db: db, now: time.Now, retention: Retention, maxRows: MaxRows}
}

// WithLimits returns a Repository with other limits, for tests of the pruning.
func (r *Repository) WithLimits(retention time.Duration, maxRows int) *Repository {
	c := *r
	c.retention, c.maxRows = retention, maxRows
	return &c
}

// WithClock returns a Repository that reads the time from now, for tests of the retention.
func (r *Repository) WithClock(now func() time.Time) *Repository {
	c := *r
	c.now = now
	return &c
}

const columns = `id, kind, target, project_id, queue_job_id, state, message, done, total, result,
	error_code, error_message, error_id, started_at, finished_at, ended_at`

// projectOf finds the Project a target belongs to. A target is the id of a Project, a Shot, a Take or an
// Edit Timeline version (every id is a UUID, so the tables cannot collide); anything else (a model, a
// workflow run, ...) belongs to none.
const projectOf = `
	SELECT id FROM project WHERE id = ?1
	UNION ALL SELECT sc.project_id FROM shot sh JOIN scene sc ON sc.id = sh.scene_id WHERE sh.id = ?1
	UNION ALL SELECT sc.project_id FROM take t JOIN prompt_version pv ON pv.id = t.prompt_version_id
		JOIN shot sh ON sh.id = pv.shot_id JOIN scene sc ON sc.id = sh.scene_id WHERE t.id = ?1
	UNION ALL SELECT project_id FROM edit_timeline_version WHERE id = ?1
	LIMIT 1`

// Insert adds a job and prunes the history. A job without a ProjectID gets the Project of its Target. A job
// that is already there (a service answers the same queue job again while it runs) keeps its first row.
func (r *Repository) Insert(ctx context.Context, e Entry) error {
	return r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if e.ProjectID == "" && e.Target != "" {
			err := tx.QueryRowContext(ctx, projectOf, e.Target).Scan(&e.ProjectID)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("find project of job target: %w", err)
			}
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO mcp_job_history (`+columns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (id) DO NOTHING`, args(e)...)
		if err != nil {
			return fmt.Errorf("insert job history: %w", err)
		}
		return r.prune(ctx, tx)
	})
}

// Settle writes what is known now about the job: its state, message, progress, result and failure, and
// when it ended. The identity (kind, target, project, start) never changes.
func (r *Repository) Settle(ctx context.Context, e Entry) error {
	return r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE mcp_job_history SET state = ?, message = ?, done = ?, total = ?, result = ?,
			error_code = ?, error_message = ?, error_id = ?, finished_at = ?, ended_at = ? WHERE id = ?`,
			e.State, e.Message, e.Done, e.Total, nullJSON(e.Result), e.ErrorCode, e.ErrorMessage, nullString(e.ErrorID),
			nullTime(e.FinishedAt), nullTime(e.EndedAt), e.ID)
		if err != nil {
			return fmt.Errorf("settle job history: %w", err)
		}
		if n, err := res.RowsAffected(); err != nil {
			return fmt.Errorf("settle job history: %w", err)
		} else if n == 0 {
			return repository.ErrNotFound // pruned meanwhile
		}
		return nil
	})
}

// Get returns the job with the id, or repository.ErrNotFound.
func (r *Repository) Get(ctx context.Context, id string) (Entry, error) {
	e, err := scan(r.db.Reader().QueryRowContext(ctx, `SELECT `+columns+` FROM mcp_job_history WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Entry{}, repository.ErrNotFound
	}
	if err != nil {
		return Entry{}, fmt.Errorf("get job history: %w", err)
	}
	return e, nil
}

// Open returns up to limit jobs that are not over, newest first.
func (r *Repository) Open(ctx context.Context, limit int) ([]Entry, error) {
	return r.query(ctx, `SELECT `+columns+` FROM mcp_job_history WHERE state IN ('queued', 'running')
		ORDER BY started_at DESC, id DESC LIMIT ?`, limit)
}

// Prune deletes the jobs past the retention and the oldest ones beyond the row limit.
func (r *Repository) Prune(ctx context.Context) error {
	return r.db.Write(ctx, r.prune)
}

func (r *Repository) prune(ctx context.Context, tx *sql.Tx) error {
	cutoff := repository.FormatTime(r.now().Add(-r.retention))
	if _, err := tx.ExecContext(ctx, `DELETE FROM mcp_job_history WHERE started_at < ?`, cutoff); err != nil {
		return fmt.Errorf("prune job history by age: %w", err)
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM mcp_job_history WHERE id IN (
		SELECT id FROM mcp_job_history ORDER BY started_at DESC, id DESC LIMIT -1 OFFSET ?)`, r.maxRows)
	if err != nil {
		return fmt.Errorf("prune job history by count: %w", err)
	}
	return nil
}

func (r *Repository) query(ctx context.Context, query string, args ...any) ([]Entry, error) {
	rows, err := r.db.Reader().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query job history: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Entry
	for rows.Next() {
		e, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("scan job history: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
