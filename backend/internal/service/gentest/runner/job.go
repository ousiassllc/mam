// Package runner runs the batches of the generation test: the handler of the job kind domain.JobGentest (every result of a batch is
// made with the direct call of its model and settled as it ends), the run of one result for the prompt match, and the cancellation of a
// batch. Package gentest embeds a Runner, so the service answers Generate and Cancel.
package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/cloudenhance"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
)

// The reasons recorded on results that did not finish.
const (
	// ReasonCancelled is the reason of a result the user cancelled before it started.
	ReasonCancelled = "キャンセルされました"
	// ReasonInterrupted is the reason of a result that was running or waiting when mam stopped.
	ReasonInterrupted = "mam が停止したため中断されました（課金される生成は再開しません）"
	// ReasonHidden is the reason of a queued result whose model was hidden before it started (the age gate of the model manager
	// was closed): the call is not made.
	ReasonHidden = "年齢確認が無効になったため、このモデルでは生成しませんでした"
)

// ErrInputHidden fails a batch that was queued with an input image taken from an adult pose (a suggestive series of the pose library) once the age
// gate is closed: nothing is run, and the message names neither the pose nor the picture.
var ErrInputHidden = errors.New("年齢確認が無効になったため、成人向けの入力画像を使う生成は行いませんでした")

// Extra runs the batches whose rounds it plans itself (the prompt match); Handle reports false for one that is not its own.
type Extra interface {
	Handle(context.Context, domain.Job, gentestrepo.Batch) (bool, error)
}

// Deps are what a Runner needs.
type Deps struct {
	Repo    *gentestrepo.Repository
	Adapter *modeladapter.Adapter
	// Dir is the directory of the history's files (<data-dir>/gentest).
	Dir string
	// Explain is the common error of a call that failed when the failure is one of the app's own refusals (a model that is not installed, a
	// license that was not accepted); nil leaves the failure to errmap. May be nil.
	Explain func(err error) *apperr.Error
	// Hidden says a model is left out of the model lists and refused as unknown (the age gate). May be nil.
	Hidden func(modelID string) bool
	// HiddenWith is Hidden for the values of the exposed inputs the result runs with (the age gate). May be nil.
	HiddenWith func(modelID string, exposed map[string]any) bool
	// Extra runs the batches that are not this runner's (nil: every batch is). The prompt match sets it once it is built, before the job queue starts.
	Extra Extra
	// InputHidden says a stored input image is out of sight (the age gate: history.Store.InputHidden): a batch that was queued with one is not run
	// once the gate is closed. May be nil.
	InputHidden func(ctx context.Context, in gentestrepo.Input) bool
	// FaceCrop cuts the face out of a reference of the role "face" for a model that reads it (nil: the picture goes as it is). See FaceCropFunc.
	FaceCrop FaceCropFunc
	// FaceScore measures the face of a result against the reference of the role "face" (nil: no number). See FaceScoreFunc.
	FaceScore FaceScoreFunc
	// FaceOff says now why the face similarity is off: what a result with a face reference records instead of a number, skipped models_missing /
	// runtime_unavailable with the words (nil, or answering nil: it is on, or nothing is recorded). The models are installed and removed while mam runs, so it is asked
	// again for every result (#1026).
	FaceOff FaceOffFunc
	// FaceSwap runs FaceFusion for a result of "swap the face" (nil: such a result fails). See FaceSwapFunc.
	FaceSwap FaceSwapFunc
	Cloud    cloudenhance.Cloud // the paid cloud of "make a video larger / smoother" (nil: such a result fails)
	Now      func() time.Time
	Logger   *slog.Logger
}

// Runner runs batches.
type Runner struct {
	Deps

	mu   sync.Mutex
	runs map[string]*run // batch id -> the batch that is running in this process
}

// New returns the Runner. Now and Logger default (the wall clock, no log).
func New(d Deps) *Runner {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Logger == nil {
		d.Logger = slog.New(slog.DiscardHandler)
	}
	return &Runner{Deps: d, runs: map[string]*run{}}
}

// run is a batch that is running in this process: the state Cancel reaches.
type run struct{ cancelled atomic.Bool }

// batchRun is what the workers of one batch share.
type batchRun struct {
	s      *Runner
	rn     *run
	dir    string
	inputs map[string]loaded
}

// loaded is an input image in memory with its role.
type loaded struct {
	role     string
	strength *float64
	position int
	in       modeladapter.Input
	// face is the picture cut to the face of a reference of the role "face" (nil: none was made, the picture goes whole).
	face *modeladapter.Input
}

// RunJob is the handler of domain.JobGentest. Every result is settled as it ends (the row, the file), so the history is
// right whenever mam stops; the results that are still queued or running at a crash are failed by OnStartup. The job itself
// fails only when the batch cannot start at all.
func (s *Runner) RunJob(ctx context.Context, job domain.Job) (err error) {
	var p record.Payload
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
	if s.Extra != nil {
		if handled, err := s.Extra.Handle(ctx, job, batch); handled {
			return err
		}
	}
	results, err := s.Repo.Results(ctx, batch.ID)
	if err != nil {
		return err
	}
	// When the batch cannot start (an input image that cannot be read or is out of sight of the age gate included), its results must not stay queued
	// for a job that is over.
	defer func() {
		if err != nil {
			_, _ = s.Repo.FailQueued(context.WithoutCancel(ctx), batch.ID, "バッチを開始できませんでした: "+err.Error(), s.Now().UTC())
		}
	}()
	inputs, err := s.loadInputs(ctx, batch.ID)
	if err != nil {
		return err
	}
	rn := &run{}
	s.mu.Lock()
	s.runs[batch.ID] = rn
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.runs, batch.ID)
		s.mu.Unlock()
	}()

	br := &batchRun{s: s, rn: rn, dir: filepath.Join(s.Dir, batch.ID), inputs: inputs}
	s.dispatch(ctx, br, queued(results))
	return s.Repo.FinishBatch(context.WithoutCancel(ctx), batch.ID, s.Now().UTC())
}

func (s *Runner) loadInputs(ctx context.Context, batchID string) (map[string]loaded, error) {
	list, err := s.Repo.Inputs(ctx, batchID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]loaded, len(list))
	for _, in := range list {
		if s.InputHidden != nil && s.InputHidden(ctx, in) { // an adult pose picture, and the age gate was closed after the batch was queued
			return nil, ErrInputHidden
		}
		data, err := os.ReadFile(filepath.Join(s.Dir, filepath.FromSlash(in.FilePath))) //nolint:gosec // the path was written by newBatch below the gentest directory
		if err != nil {
			return nil, fmt.Errorf("read input image %s: %w", in.ID, err)
		}
		l := loaded{role: in.Role, strength: in.Strength, position: in.Position, in: modeladapter.Input{Data: data, MimeType: in.MimeType}}
		l.face = s.cropFace(ctx, l)
		out[in.ID] = l
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
func (s *Runner) dispatch(ctx context.Context, br *batchRun, results []gentestrepo.Result) {
	byVendor := map[modeladapter.Vendor][]gentestrepo.Result{}
	cfg := s.Adapter.Config()
	for _, r := range results {
		v := cfg.VendorOf(domain.ModelName(r.Model)) // the selected API provider's vendor for a model it serves
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

// Generate runs the queued result r now (the prompt match writes the prompt of its rounds as it goes): it is made and settled as in a batch.
func (s *Runner) Generate(ctx context.Context, r gentestrepo.Result) {
	(&batchRun{s: s, rn: &run{}, dir: filepath.Join(s.Dir, r.BatchID)}).start(ctx, r)
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
	started, err := s.Repo.MarkRunning(ctx, r.ID, s.Now().UTC())
	if err != nil {
		s.Logger.Error("mark generation test result running failed", "result_id", r.ID, "err", err)
		return
	}
	if !started {
		return
	}
	if s.Hidden != nil && s.Hidden(r.Model) { // the age gate was closed after the batch was queued: the call is not made
		s.settle(ctx, r.ID, gentestrepo.StatusFailed, ReasonHidden, nil)
		return
	}
	br.execute(ctx, r)
}

// settle records how a result ended, whatever happened to ctx. A result that is over already is left as it is.
func (s *Runner) settle(ctx context.Context, id, status, reason string, set func(*gentestrepo.Settlement)) {
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
func (s *Runner) Cancel(ctx context.Context, batchID string) (CancelResult, error) {
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
