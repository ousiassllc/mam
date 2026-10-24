package modelmanager

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/track"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// JobStatus is the state of an install or trial job: the job table says queued, running, succeeded or failed;
// Progress is what the running job reported (in memory: a job resumed after a restart starts again from its first
// step); Result is the trial of a succeeded gentest job. The MCP get_job Probe of these kinds is built on it.
type JobStatus struct {
	JobID string         `json:"job_id"`
	Kind  domain.JobKind `json:"kind"`
	// Target is "engine", "model:<catalog id>" or "gentest:<model id>".
	Target     string           `json:"target"`
	State      domain.JobStatus `json:"state"`
	Progress   track.Update     `json:"progress"`
	Error      string           `json:"error,omitempty"`
	Attempts   int              `json:"attempts"`
	Result     *GentestRecord   `json:"result,omitempty"`
	CreatedAt  time.Time        `json:"created_at"`
	StartedAt  *time.Time       `json:"started_at,omitempty"`
	FinishedAt *time.Time       `json:"finished_at,omitempty"`
}

// JobStatus reads a job of the model manager (ErrNotFound for any other).
func (s *Service) JobStatus(ctx context.Context, jobID string) (JobStatus, error) {
	job, err := s.JobTable.Get(ctx, jobID)
	if errors.Is(err, repository.ErrNotFound) {
		return JobStatus{}, ErrNotFound
	}
	if err != nil {
		return JobStatus{}, err
	}
	if job.Kind != domain.JobModelInstall && job.Kind != domain.JobGentest {
		return JobStatus{}, ErrNotFound
	}
	st := JobStatus{
		JobID: job.ID, Kind: job.Kind, State: job.Status, Error: job.Error, Attempts: job.Attempts,
		CreatedAt: job.CreatedAt, StartedAt: job.StartedAt, FinishedAt: job.FinishedAt, Target: payloadTarget(job),
	}
	if e, ok := s.tracker.Get(jobID); ok {
		st.Progress = e.Update
	}
	if job.Status == domain.JobSucceeded {
		st.Progress.Percent = 100
		if job.Kind == domain.JobGentest {
			if rec, err := s.Gentest(ctx, jobID); err == nil {
				st.Result = &rec
			}
		}
	}
	return st, nil
}

// payloadTarget is what the job works on, from its payload.
func payloadTarget(job domain.Job) string {
	var p struct {
		Target  string `json:"target"`
		ModelID string `json:"model_id"`
	}
	_ = json.Unmarshal(job.Payload, &p)
	if job.Kind == domain.JobGentest {
		return "gentest:" + p.ModelID
	}
	return p.Target
}
