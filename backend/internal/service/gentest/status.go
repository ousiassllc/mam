package gentest

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
)

// The state of a batch (BatchStatus.State).
const (
	StateQueued    = "queued"
	StateRunning   = "running"
	StateSucceeded = "succeeded" // every result is over and at least one made a file
	StateFailed    = "failed"    // every result is over and none made a file
	StateCancelled = "cancelled" // some results were cancelled before they started
)

// BatchStatus is a batch with its progress (counted from the results: HTMX and the MCP status tool poll it) and, when asked,
// every result.
type BatchStatus struct {
	BatchID string `json:"batch_id"`
	JobID   string `json:"job_id"`
	Media   string `json:"media"`
	State   string `json:"state"`
	// Total is the number of results; Done the number that are over (succeeded, failed or cancelled).
	Total     int `json:"total"`
	Done      int `json:"done"`
	Queued    int `json:"queued"`
	Running   int `json:"running"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
	Cancelled int `json:"cancelled"`
	// Finished says no result can change any more.
	Finished     bool         `json:"finished"`
	Error        string       `json:"error,omitempty"`
	EstimatedUSD float64      `json:"estimated_usd"`
	AspectRatio  string       `json:"aspect_ratio"`
	Estimate     *Estimate    `json:"estimate,omitempty"`
	Results      []ResultView `json:"results,omitempty"`
	CreatedAt    time.Time    `json:"created_at"`
	FinishedAt   *time.Time   `json:"finished_at,omitempty"`
}

// Batch returns a batch with its progress and, with results true, every result in the order they were planned (gterr.ErrNotFound).
func (s *Service) Batch(ctx context.Context, id string, results bool) (BatchStatus, error) {
	b, err := s.Repo.GetBatch(ctx, id)
	if err != nil {
		return BatchStatus{}, gterr.NotFound(err)
	}
	return s.status(ctx, b, results)
}

// BatchOfJob returns the batch a job runs (gterr.ErrNotFound for a job that is not a generation test).
func (s *Service) BatchOfJob(ctx context.Context, jobID string, results bool) (BatchStatus, error) {
	b, err := s.Repo.BatchByJob(ctx, jobID)
	if err != nil {
		return BatchStatus{}, gterr.NotFound(err)
	}
	return s.status(ctx, b, results)
}

// Batches lists the latest batches (without their results), newest first.
func (s *Service) Batches(ctx context.Context, limit int) ([]BatchStatus, error) {
	list, err := s.Repo.ListBatches(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := make([]BatchStatus, 0, len(list))
	for _, b := range list {
		st, err := s.status(ctx, b, false)
		if err != nil {
			return nil, err
		}
		st.Estimate = nil
		out = append(out, st)
	}
	return out, nil
}

func (s *Service) status(ctx context.Context, b gentestrepo.Batch, results bool) (BatchStatus, error) {
	c, err := s.Repo.Counts(ctx, b.ID)
	if err != nil {
		return BatchStatus{}, err
	}
	var p batchParams
	_ = json.Unmarshal(b.Params, &p)
	st := BatchStatus{
		BatchID: b.ID, JobID: b.JobID, Media: b.Media, Total: c.Total(), Done: c.Done(), Queued: c.Queued, Running: c.Running,
		Succeeded: c.Succeeded, Failed: c.Failed, Cancelled: c.Cancelled, EstimatedUSD: b.EstimatedCost, AspectRatio: p.AspectRatio,
		CreatedAt: b.CreatedAt, FinishedAt: b.FinishedAt,
	}
	if len(b.Estimate) > 0 {
		var e Estimate
		if json.Unmarshal(b.Estimate, &e) == nil {
			st.Estimate = &e
		}
	}
	st.State, st.Error = s.stateOf(ctx, b, c)
	st.Finished = st.State == StateSucceeded || st.State == StateFailed || st.State == StateCancelled
	if results {
		list, err := s.Repo.Results(ctx, b.ID)
		if err != nil {
			return BatchStatus{}, err
		}
		st.Results = viewsOf(list)
	}
	return st, nil
}

// stateOf derives the state from the results and the job. A job that failed leaves its reason (the results it did not reach
// were failed with it).
func (s *Service) stateOf(ctx context.Context, b gentestrepo.Batch, c gentestrepo.Counts) (state, reason string) {
	if c.Queued+c.Running > 0 {
		if c.Running == 0 && c.Done() == 0 && s.jobStatus(ctx, b.JobID) == domain.JobQueued {
			return StateQueued, ""
		}
		return StateRunning, ""
	}
	switch {
	case c.Cancelled > 0:
		return StateCancelled, ""
	case c.Succeeded == 0 && c.Failed > 0:
		return StateFailed, s.jobError(ctx, b.JobID)
	}
	return StateSucceeded, ""
}

func (s *Service) jobStatus(ctx context.Context, jobID string) domain.JobStatus {
	if s.JobTable == nil || jobID == "" {
		return ""
	}
	job, err := s.JobTable.Get(ctx, jobID)
	if err != nil {
		return ""
	}
	return job.Status
}

func (s *Service) jobError(ctx context.Context, jobID string) string {
	if s.JobTable == nil || jobID == "" {
		return ""
	}
	job, err := s.JobTable.Get(ctx, jobID)
	if err != nil || job.Status != domain.JobFailed {
		return ""
	}
	return job.Error
}

// JobKind reports whether the job is a generation test job; the MCP probe and get_job use it.
func (s *Service) JobKind(ctx context.Context, jobID string) bool {
	job, err := s.JobTable.Get(ctx, jobID)
	return err == nil && job.Kind == domain.JobGentest
}
