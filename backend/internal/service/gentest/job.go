package gentest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
)

// run is a batch that is running in this process: the state Cancel reaches.
type run struct{ cancelled atomic.Bool }

// batchRun is what the workers of one batch share.
type batchRun struct {
	s      *Service
	rn     *run
	dir    string
	inputs map[string]loaded
}

// loaded is an input image in memory with its role.
type loaded struct {
	role     string
	position int
	in       modeladapter.Input
}

// handle is the handler of domain.JobGentest. Every result is settled as it ends (the row, the file), so the history is
// right whenever mam stops; the results that are still queued or running at a crash are failed by OnStartup. The job itself
// fails only when the batch cannot start at all.
func (s *Service) handle(ctx context.Context, job domain.Job) (err error) {
	var p payload
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return fmt.Errorf("bad payload: %w", err)
	}
	batch, err := s.Repo.GetBatch(ctx, p.BatchID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil // the batch was deleted before the job started
	}
	if err != nil {
		return err
	}
	results, err := s.Repo.Results(ctx, batch.ID)
	if err != nil {
		return err
	}
	inputs, err := s.loadInputs(ctx, batch.ID)
	if err != nil {
		return err
	}
	// When the batch cannot start, its results must not stay queued for a job that is over.
	defer func() {
		if err != nil {
			_, _ = s.Repo.FailQueued(context.WithoutCancel(ctx), batch.ID, "バッチを開始できませんでした: "+err.Error(), s.Now().UTC())
		}
	}()
	rn := &run{}
	s.mu.Lock()
	s.runs[batch.ID] = rn
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.runs, batch.ID)
		s.mu.Unlock()
	}()

	br := &batchRun{s: s, rn: rn, dir: filepath.Join(s.dir(), batch.ID), inputs: inputs}
	s.dispatch(ctx, br, queued(results))
	return s.Repo.FinishBatch(context.WithoutCancel(ctx), batch.ID, s.Now().UTC())
}

func (s *Service) loadInputs(ctx context.Context, batchID string) (map[string]loaded, error) {
	list, err := s.Repo.Inputs(ctx, batchID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]loaded, len(list))
	for _, in := range list {
		data, err := os.ReadFile(filepath.Join(s.dir(), filepath.FromSlash(in.FilePath))) //nolint:gosec // the path was written by newBatch below the gentest directory
		if err != nil {
			return nil, fmt.Errorf("read input image %s: %w", in.ID, err)
		}
		out[in.ID] = loaded{role: in.Role, position: in.Position, in: modeladapter.Input{Data: data, MimeType: in.MimeType}}
	}
	return out, nil
}

// queued are the results that have not started.
func queued(results []gentestrepo.Result) []gentestrepo.Result {
	var out []gentestrepo.Result
	for _, r := range results {
		if r.Status == gentestrepo.StatusQueued {
			out = append(out, r)
		}
	}
	return out
}

// dispatch runs the results with as many workers per vendor as the vendor's concurrency limit allows now; the vendors run
// side by side. A result is started only while the batch is not cancelled and mam is not stopping.
func (s *Service) dispatch(ctx context.Context, br *batchRun, results []gentestrepo.Result) {
	byVendor := map[modeladapter.Vendor][]gentestrepo.Result{}
	for _, r := range results {
		v := modeladapter.VendorOf(domain.ModelName(r.Model))
		byVendor[v] = append(byVendor[v], r)
	}
	var wg sync.WaitGroup
	for v, list := range byVendor {
		next := make(chan gentestrepo.Result)
		workers := max(1, s.Adapter.ConcurrencyLimit(v))
		for range min(workers, len(list)) {
			wg.Go(func() {
				for r := range next {
					br.start(ctx, r)
				}
			})
		}
		wg.Go(func() {
			defer close(next)
			for _, r := range list {
				next <- r
			}
		})
	}
	wg.Wait()
}

// start runs one result unless the batch was cancelled or mam is stopping.
func (br *batchRun) start(ctx context.Context, r gentestrepo.Result) {
	s := br.s
	switch {
	case br.rn.cancelled.Load():
		return // Cancel marked it cancelled already
	case ctx.Err() != nil:
		s.settle(ctx, r.ID, gentestrepo.StatusFailed, ReasonInterrupted, nil)
		return
	}
	started, err := s.Repo.MarkRunning(ctx, r.ID)
	if err != nil {
		s.Logger.Error("mark generation test result running failed", "result_id", r.ID, "err", err)
		return
	}
	if !started {
		return
	}
	br.execute(ctx, r)
}

// settle records how a result ended, whatever happened to ctx. A result that is over already is left as it is.
func (s *Service) settle(ctx context.Context, id, status, reason string, set func(*gentestrepo.Settlement)) {
	st := gentestrepo.Settlement{Status: status, Error: reason, At: s.Now().UTC()}
	if set != nil {
		set(&st)
	}
	if err := s.Repo.Settle(context.WithoutCancel(ctx), id, st); err != nil && !errors.Is(err, repository.ErrNotFound) {
		s.Logger.Error("settle generation test result failed", "result_id", id, "err", err)
	}
}

// CancelResult is what Cancel did.
type CancelResult struct {
	// Cancelled is the number of results that had not started and will not.
	Cancelled int `json:"cancelled"`
	// Running is the number of results whose vendor call is in flight: it finishes (and is billed) and keeps its result.
	Running int `json:"running"`
}

// Cancel stops a batch from starting more results: the results that have not started are cancelled, the ones whose vendor call
// is in flight finish. A batch that is over is a *gterr.ConflictError.
func (s *Service) Cancel(ctx context.Context, batchID string) (CancelResult, error) {
	if _, err := s.Repo.GetBatch(ctx, batchID); err != nil {
		return CancelResult{}, gterr.NotFound(err)
	}
	counts, err := s.Repo.Counts(ctx, batchID)
	if err != nil {
		return CancelResult{}, err
	}
	if counts.Queued+counts.Running == 0 {
		return CancelResult{}, gterr.Conflict("このバッチはすでに終わっています")
	}
	s.mu.Lock()
	rn := s.runs[batchID]
	s.mu.Unlock()
	if rn != nil {
		rn.cancelled.Store(true)
	}
	n, err := s.Repo.CancelQueued(ctx, batchID, ReasonCancelled, s.Now().UTC())
	return CancelResult{Cancelled: n, Running: counts.Running}, err
}
