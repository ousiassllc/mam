// Package jobqueue runs generation / Judge / Repair work in background goroutines.
//
// Jobs are persisted (table job) before they are run, so after an abnormal
// termination Recover can find the jobs that were in flight and either resume
// them or fail them safely (non-functional.md §2, §3). All state changes go
// through repository.DB.Write, i.e. the single writer.
package jobqueue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// DefaultWorkers is the number of concurrent workers when Options.Workers is zero.
const DefaultWorkers = 4

// DefaultMaxAttempts bounds how often an interrupted resumable job is restarted.
const DefaultMaxAttempts = 3

// InterruptedReason is recorded on jobs that were running when the app terminated abnormally.
const InterruptedReason = "interrupted by abnormal termination"

// Handler executes one job. It returns nil on success. When ctx is canceled
// (shutdown) it SHOULD return promptly; the job is then queued again.
type Handler func(ctx context.Context, job domain.Job) error

// Options configures a Queue.
type Options struct {
	Workers     int
	MaxAttempts int
	Logger      *slog.Logger
}

type registration struct {
	handle    Handler
	resumable bool
}

// Queue is the in-process background job queue.
type Queue struct {
	repo        *repository.JobRepository
	logger      *slog.Logger
	workers     int
	maxAttempts int

	mu       sync.Mutex
	handlers map[domain.JobKind]registration
	onChange func(jobID string)
	started  bool

	wake   chan struct{}
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New returns a Queue that persists jobs through repo.
func New(repo *repository.JobRepository, opts Options) *Queue {
	if opts.Workers <= 0 {
		opts.Workers = DefaultWorkers
	}
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = DefaultMaxAttempts
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	return &Queue{
		repo:        repo,
		logger:      opts.Logger,
		workers:     opts.Workers,
		maxAttempts: opts.MaxAttempts,
		handlers:    make(map[domain.JobKind]registration),
		wake:        make(chan struct{}, 1),
	}
}

// Register installs the handler for kind. It MUST be called before Start.
//
// resumable tells Recover what to do with a job of this kind found running after
// an abnormal termination: true re-runs it (the handler MUST be idempotent, e.g.
// write outputs atomically and reuse existing rows); false marks it failed so no
// half-done Take/State is continued and no paid API call is repeated silently.
func (q *Queue) Register(kind domain.JobKind, handler Handler, resumable bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.started {
		panic("jobqueue: Register called after Start")
	}
	q.handlers[kind] = registration{handle: handler, resumable: resumable}
}

// OnChange installs a callback invoked after every status change of a job (queued,
// running, succeeded, failed, requeued) with the job ID; the chat uses it to refresh
// progress cards. fn runs on the caller's goroutine and MUST NOT block. It MUST be
// called before Start.
func (q *Queue) OnChange(fn func(jobID string)) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.started {
		panic("jobqueue: OnChange called after Start")
	}
	q.onChange = fn
}

func (q *Queue) changed(jobID string) {
	q.mu.Lock()
	fn := q.onChange
	q.mu.Unlock()
	if fn != nil {
		fn(jobID)
	}
}

// Enqueue persists a new job and wakes a worker. It fails if no handler is registered for kind.
func (q *Queue) Enqueue(ctx context.Context, kind domain.JobKind, payload any) (domain.Job, error) {
	q.mu.Lock()
	_, ok := q.handlers[kind]
	q.mu.Unlock()
	if !ok {
		return domain.Job{}, fmt.Errorf("jobqueue: no handler registered for %q", kind)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return domain.Job{}, fmt.Errorf("marshal job payload: %w", err)
	}
	job, err := q.repo.Create(ctx, kind, raw)
	if err != nil {
		return domain.Job{}, err
	}
	q.signal()
	q.changed(job.ID)
	return job, nil
}

// Recover resolves jobs left in the running state by an abnormal termination:
// resumable ones (below MaxAttempts) are queued again, all others are failed.
// It returns the number of resumed and failed jobs. Call it before Start.
func (q *Queue) Recover(ctx context.Context) (resumed, failed int, err error) {
	jobs, err := q.repo.ListByStatus(ctx, domain.JobRunning)
	if err != nil {
		return 0, 0, err
	}
	for _, job := range jobs {
		q.mu.Lock()
		reg, ok := q.handlers[job.Kind]
		q.mu.Unlock()

		switch {
		case ok && reg.resumable && job.Attempts < q.maxAttempts:
			if err := q.repo.Requeue(ctx, job.ID); err != nil {
				return resumed, failed, err
			}
			resumed++
			q.logger.Warn("resuming interrupted job", "job_id", job.ID, "kind", job.Kind, "attempts", job.Attempts)
		default:
			if err := q.repo.MarkFailed(ctx, job.ID, InterruptedReason); err != nil {
				return resumed, failed, err
			}
			failed++
			q.logger.Warn("failed interrupted job", "job_id", job.ID, "kind", job.Kind, "attempts", job.Attempts)
		}
	}
	return resumed, failed, nil
}

// Start launches the workers. Jobs already queued (including those resumed by Recover) are picked up.
func (q *Queue) Start(ctx context.Context) {
	q.mu.Lock()
	if q.started {
		q.mu.Unlock()
		return
	}
	q.started = true
	ctx, q.cancel = context.WithCancel(ctx)
	q.mu.Unlock()

	for range q.workers {
		q.wg.Go(func() { q.work(ctx) })
	}
	q.signal()
}

// Stop cancels running handlers, waits for the workers to finish and
// returns interrupted jobs to the queue so the next start resumes them.
func (q *Queue) Stop() {
	q.mu.Lock()
	cancel := q.cancel
	q.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	q.wg.Wait()
}

func (q *Queue) signal() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *Queue) work(ctx context.Context) {
	for {
		job, err := q.repo.ClaimNext(ctx)
		switch {
		case err == nil:
			// Another worker may also have a job waiting: pass the wake-up on.
			q.signal()
			q.changed(job.ID)
			q.run(ctx, job)
			q.changed(job.ID)
			continue
		case errors.Is(err, repository.ErrNotFound):
		case ctx.Err() != nil:
			return
		default:
			q.logger.Error("claim job failed", "err", err)
		}

		select {
		case <-q.wake:
		case <-ctx.Done():
			return
		}
	}
}

func (q *Queue) run(ctx context.Context, job domain.Job) {
	// Status updates must survive shutdown so that no job stays "running" needlessly.
	settle := context.WithoutCancel(ctx)

	q.mu.Lock()
	reg, ok := q.handlers[job.Kind]
	q.mu.Unlock()
	if !ok {
		q.fail(settle, job, fmt.Sprintf("no handler registered for %q", job.Kind))
		return
	}

	err := q.invoke(ctx, reg.handle, job)
	switch {
	case err == nil:
		if err := q.repo.MarkSucceeded(settle, job.ID); err != nil {
			q.logger.Error("mark job succeeded failed", "job_id", job.ID, "err", err)
		}
	case ctx.Err() != nil && errors.Is(err, ctx.Err()):
		if err := q.repo.Requeue(settle, job.ID); err != nil {
			q.logger.Error("requeue interrupted job failed", "job_id", job.ID, "err", err)
		}
	default:
		q.fail(settle, job, err.Error())
	}
}

func (q *Queue) invoke(ctx context.Context, handle Handler, job domain.Job) (err error) {
	defer func() {
		if r := recover(); r != nil {
			q.logger.Error("job handler panicked", "job_id", job.ID, "kind", job.Kind, "panic", r, "stack", string(debug.Stack()))
			err = fmt.Errorf("handler panicked: %v", r)
		}
	}()
	return handle(ctx, job)
}

func (q *Queue) fail(ctx context.Context, job domain.Job, reason string) {
	q.logger.Error("job failed", "job_id", job.ID, "kind", job.Kind, "reason", reason)
	if err := q.repo.MarkFailed(ctx, job.ID, reason); err != nil {
		q.logger.Error("mark job failed failed", "job_id", job.ID, "err", err)
	}
}
