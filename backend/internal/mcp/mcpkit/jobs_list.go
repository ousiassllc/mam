package mcpkit

import (
	"context"
	"errors"
	"time"

	"github.com/ousiassllc/mam/backend/internal/logging"
	"github.com/ousiassllc/mam/backend/internal/repository/jobhistory"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// JobSummary is one job of a listing: what get_job answers, without the result (ask get_job for it). The
// message and the failure pass through logging.Redact: the listing is for searching, and is not to carry a secret
// that a service put into a reason.
type JobSummary struct {
	JobID      string     `json:"job_id"`
	Kind       string     `json:"kind"`
	Target     string     `json:"target,omitempty"`
	ProjectID  string     `json:"project_id,omitempty"`
	State      JobState   `json:"state"`
	Message    string     `json:"message,omitempty"`
	Done       int        `json:"done,omitempty"`
	Total      int        `json:"total,omitempty"`
	Error      string     `json:"error,omitempty"`
	ErrorCode  string     `json:"error_code,omitempty"`
	ErrorID    string     `json:"error_id,omitempty" jsonschema:"internal の失敗の相関 ID（get_app_logs の attrs error_id でログ行をたどる）"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	EndedAt    *time.Time `json:"ended_at,omitempty"`
}

// JobQuery selects the jobs of a listing. Every field is optional.
type JobQuery struct {
	Kind      string
	State     JobState
	ErrorCode string
	ProjectID string
	// Since is the oldest start (inclusive) and Until the newest (exclusive).
	Since, Until time.Time
	// Cursor is the Next of the previous page.
	Cursor string
	Limit  int
}

// JobPage is one page of a listing, newest start first.
type JobPage struct {
	Jobs []JobSummary
	// Next is the cursor of the next page, "" on the last one.
	Next string
}

// List returns the jobs in the history that match q, newest start first. The jobs that are not over are
// looked at again first (RefreshOpen), so that a job that has ended shows its end and can be found by its
// state and failure.
func (j *Jobs) List(ctx context.Context, q JobQuery) (JobPage, error) {
	if err := j.RefreshOpen(ctx); err != nil {
		return JobPage{}, err
	}
	page, err := j.history.List(ctx, jobhistory.Filter{
		Kind: q.Kind, State: string(q.State), ErrorCode: q.ErrorCode, ProjectID: q.ProjectID,
		Since: q.Since, Until: q.Until, After: q.Cursor, Limit: q.Limit,
	})
	if errors.Is(err, jobhistory.ErrBadCursor) {
		return JobPage{}, &service.ValidationError{Field: "cursor", Message: "前のページの next_cursor をそのまま渡してください"}
	}
	if err != nil {
		return JobPage{}, err
	}
	out := JobPage{Jobs: make([]JobSummary, len(page.Entries)), Next: page.Next}
	for i, e := range page.Entries {
		st := statusFrom(e)
		out.Jobs[i] = JobSummary{
			JobID: st.JobID, Kind: st.Kind, Target: st.Target, ProjectID: st.ProjectID, State: st.State, Message: logging.Redact(st.Message),
			Done: st.Done, Total: st.Total, Error: logging.Redact(st.Error), ErrorCode: st.ErrorCode, ErrorID: st.ErrorID,
			StartedAt: st.StartedAt, FinishedAt: st.FinishedAt, EndedAt: st.EndedAt,
		}
	}
	return out, nil
}
