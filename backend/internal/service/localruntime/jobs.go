package localruntime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/track"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// JobStatus is the state of the install job of a runtime: the job table says queued, running, succeeded or failed; Progress is
// what the running job reported (in memory: a job resumed after a restart starts again from its first step). It has the shape of
// modelmanager.JobStatus.
type JobStatus struct {
	JobID string         `json:"job_id"`
	Kind  domain.JobKind `json:"kind"`
	// Target is "runtime:<id>".
	Target     string           `json:"target"`
	State      domain.JobStatus `json:"state"`
	Progress   track.Update     `json:"progress"`
	Error      string           `json:"error,omitempty"`
	Attempts   int              `json:"attempts"`
	CreatedAt  time.Time        `json:"created_at"`
	StartedAt  *time.Time       `json:"started_at,omitempty"`
	FinishedAt *time.Time       `json:"finished_at,omitempty"`
}

// JobStatus reads the install job of a runtime (ErrNotFound for any other job).
func (s *Service) JobStatus(ctx context.Context, jobID string) (JobStatus, error) {
	job, err := s.JobTable.Get(ctx, jobID)
	if errors.Is(err, repository.ErrNotFound) {
		return JobStatus{}, ErrNotFound
	}
	if err != nil {
		return JobStatus{}, err
	}
	var p installPayload
	if job.Kind != domain.JobModelInstall || json.Unmarshal(job.Payload, &p) != nil || !strings.HasPrefix(p.Target, TargetPrefix) {
		return JobStatus{}, ErrNotFound
	}
	st := JobStatus{
		JobID: job.ID, Kind: job.Kind, Target: p.Target, State: job.Status, Error: job.Error, Attempts: job.Attempts,
		CreatedAt: job.CreatedAt, StartedAt: job.StartedAt, FinishedAt: job.FinishedAt,
	}
	if e, ok := s.tracker.Get(jobID); ok {
		st.Progress = e.Update
	}
	if job.Status == domain.JobSucceeded {
		st.Progress.Percent = 100
	}
	return st, nil
}
