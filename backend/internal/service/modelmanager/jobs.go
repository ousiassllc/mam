package modelmanager

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/track"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// JobStatus is the state of an install job: the job table says queued, running, succeeded or failed; Progress is what
// the running job reported (in memory: a job resumed after a restart starts again from its first step). The MCP get_job
// Probe of the kind "model_install" is built on it. (The trial generations are batches of the generation test service.)
type JobStatus struct {
	JobID string         `json:"job_id"`
	Kind  domain.JobKind `json:"kind"`
	// Target is "engine", "model:<catalog id>" or "hf:<repo>@<commit>/<path>".
	Target   string           `json:"target"`
	State    domain.JobStatus `json:"state"`
	Progress track.Update     `json:"progress"`
	Error    string           `json:"error,omitempty"`
	// Failure is the failure as the common error in words (#1010), when the job kept one (job.error_info); nil otherwise.
	Failure    *apperr.View `json:"failure,omitempty"`
	Attempts   int          `json:"attempts"`
	CreatedAt  time.Time    `json:"created_at"`
	StartedAt  *time.Time   `json:"started_at,omitempty"`
	FinishedAt *time.Time   `json:"finished_at,omitempty"`
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
	if job.Kind != domain.JobModelInstall {
		return JobStatus{}, ErrNotFound
	}
	st := JobStatus{
		JobID: job.ID, Kind: job.Kind, State: job.Status, Error: job.Error, Failure: apperr.ViewOf(job.ErrorInfo, apperr.LangJa), Attempts: job.Attempts,
		CreatedAt: job.CreatedAt, StartedAt: job.StartedAt, FinishedAt: job.FinishedAt, Target: payloadTarget(job),
	}
	if e, ok := s.tracker.Get(jobID); ok {
		st.Progress = e.Update
	}
	if job.Status == domain.JobSucceeded {
		st.Progress.Percent = 100
	}
	return st, nil
}

// payloadTarget is what the job works on, from its payload.
func payloadTarget(job domain.Job) string {
	var p struct {
		Target string `json:"target"`
	}
	_ = json.Unmarshal(job.Payload, &p)
	return p.Target
}

// errCancelled is why an install the user stopped failed. The job is failed, not requeued: the user starts the
// install again, which continues from the .part files.
var errCancelled = apperr.WithText("導入を中断しました。もう一度導入すると、ダウンロード済みの続きから再開します", apperr.New(apperr.CodeInstallCancelled))

// cancellable returns ctx with a cancel that CancelInstall reaches by the id of the job, and what to call when the
// handler ends.
func (s *Service) cancellable(ctx context.Context, jobID string) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(ctx)
	s.mu.Lock()
	s.cancels[jobID] = cancel
	s.mu.Unlock()
	return ctx, func() {
		s.mu.Lock()
		delete(s.cancels, jobID)
		s.mu.Unlock()
		cancel(nil)
	}
}

// CancelInstall stops a running install (the engine's or a model's) by the id of its job. The job ends as failed
// with a message that says so; the files downloaded so far stay, and installing again resumes from them. A job
// that is not running (queued, ended, or not an install) is ErrNotFound.
func (s *Service) CancelInstall(jobID string) error {
	s.mu.Lock()
	cancel, ok := s.cancels[jobID]
	s.mu.Unlock()
	if !ok {
		return ErrNotFound
	}
	cancel(errCancelled)
	return nil
}
