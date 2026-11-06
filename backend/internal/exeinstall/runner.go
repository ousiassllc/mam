package exeinstall

import (
	"context"
	"errors"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/worktrack"
)

// State is where the install of a downloaded program stands.
type State string

// The states of an install.
const (
	Idle      State = "idle"
	Running   State = "running"
	Succeeded State = "succeeded"
	Failed    State = "failed"
)

// Status is the progress of the install of a downloaded program (Codex, ffmpeg).
type Status struct {
	State State
	// Phase is the step the install is in (download, extract, ...): the words of the program's own installer.
	Phase   string
	Percent int
	Done    int64
	Total   int64
	Message string
	// Error says why the install failed, for the user. Cancelled: the user stopped it (not a failure of the program).
	Error     string
	Cancelled bool
}

// RunnerConfig is what differs between the programs that a Runner installs.
type RunnerConfig struct {
	// Kind and Subject name the install in the job list (worktrack).
	Kind    worktrack.Kind
	Subject string
	// Starting is the status while the install starts (its Phase and Message; State is Running).
	Starting Status
	// ErrClosed is what Start returns once Close was called; ErrRemoving what it returns while the files are held for a removal (Hold).
	ErrClosed, ErrRemoving error
	// Preflight runs after the install is marked running and before it is started: the checks that refuse at once (a directory that
	// cannot be written to, no room on the disk) and whatever must stop before the files are replaced. An error ends Start and
	// restores the status as it was.
	Preflight func() error
	// Install does the work. It calls report with the progress of the step it is in (Phase, Percent, Done, Total, Message; the
	// State is the Runner's) and returns when it is done or ctx was cancelled (the error wraps context.Canceled then).
	Install func(ctx context.Context, report func(Status)) error
	// Succeeded is the Message of a finished install.
	Succeeded string
	// Cancelled is the status of an install the user stopped (State Failed and Cancelled true are the Runner's to set).
	Cancelled Status
	// Failed words the error of an install that failed, for the user. It is also where the failure is logged.
	Failed func(err error) string
}

// Runner is the install of a downloaded program in the background: the one that may run at a time, its progress, and the
// stop and the shutdown of it. Codex (chatgpt/codex) and ffmpeg (media/ffmpegdl) share it, so that they start, stop and report
// in one way. The zero Runner is not usable: use NewRunner.
type Runner struct {
	cfg   RunnerConfig
	works worktrack.Tracker

	mu     sync.Mutex
	status Status
	cancel context.CancelFunc
	closed bool
	// removing: the files are being deleted (Hold); Start refuses so that no install starts under the deletion.
	removing bool
	wg       sync.WaitGroup
}

// NewRunner is a Runner of the program that cfg describes.
func NewRunner(cfg RunnerConfig) *Runner { return &Runner{cfg: cfg} }

// Status is the progress of the install (State Idle when none was started).
func (r *Runner) Status() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status.State == "" {
		return Status{State: Idle}
	}
	return r.status
}

// Start runs the install in the background (progress: Status). started is false when an install is already running; an error is
// ErrClosed, ErrRemoving or what Preflight refused with, and leaves the status as it was.
func (r *Runner) Start() (started bool, err error) {
	// The status is set under the lock of the check, so that two callers cannot both start, and what watches it (the start of
	// the program) refuses from then on: the checks below take a while.
	ctx, cancel := context.WithCancel(context.Background())
	r.mu.Lock()
	if refused := r.refusal(); refused != nil || r.status.State == Running {
		r.mu.Unlock()
		cancel()
		return false, refused
	}
	prev := r.status
	r.status = r.cfg.Starting
	r.status.State = Running
	r.cancel = cancel
	r.wg.Add(1)
	r.mu.Unlock()
	if r.cfg.Preflight != nil {
		if err := r.cfg.Preflight(); err != nil {
			r.mu.Lock()
			r.status, r.cancel = prev, nil
			r.mu.Unlock()
			r.wg.Done()
			cancel()
			return false, err
		}
	}
	go r.run(ctx, r.works.Begin(worktrack.Record{Kind: r.cfg.Kind, Subject: r.cfg.Subject}, r.cfg.Starting.Message))
	return true, nil
}

// refusal is why no install may start now (nil when none): called with the lock held.
func (r *Runner) refusal() error {
	switch {
	case r.closed:
		return r.cfg.ErrClosed
	case r.removing:
		return r.cfg.ErrRemoving
	}
	return nil
}

func (r *Runner) run(ctx context.Context, work *worktrack.Run) {
	defer r.wg.Done()
	err := r.cfg.Install(ctx, func(p Status) {
		p.State = Running
		r.mu.Lock()
		r.status = p
		r.mu.Unlock()
		work.Progress(p.Percent, p.Message)
	})
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cancel = nil
	switch {
	case err == nil:
		r.status = Status{State: Succeeded, Percent: 100, Message: r.cfg.Succeeded}
		work.Succeed()
	case errors.Is(err, context.Canceled):
		r.status = r.cfg.Cancelled
		r.status.State, r.status.Cancelled = Failed, true
		work.Cancel()
	default:
		r.status = Status{State: Failed, Error: r.cfg.Failed(err)}
		work.Fail(r.status.Error)
	}
}

// Works is the installs of this process, for the job list (worktrack.Source): the one that runs and the latest that ended. It is
// memory only: a restart forgets them.
func (r *Runner) Works() []worktrack.Record { return r.works.Works() }

// Cancel stops an install in progress; what it downloaded stays, so that installing again resumes.
func (r *Runner) Cancel() {
	r.mu.Lock()
	cancel := r.cancel
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Wait waits for the install in progress to end (tests and shutdown).
func (r *Runner) Wait() { r.wg.Wait() }

// Close stops an install in progress and refuses new ones (shutdown), and waits for the install to end.
func (r *Runner) Close() {
	r.mu.Lock()
	r.closed = true
	cancel := r.cancel
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	r.wg.Wait()
}

// Hold marks the files as being deleted: Start refuses (ErrRemoving) until release is called. With exclusive, it is refused (ok is
// false) while an install is running or another hold is in force; without, it holds anyway (the caller stops the install).
func (r *Runner) Hold(exclusive bool) (release func(), ok bool) {
	r.mu.Lock()
	if exclusive && (r.status.State == Running || r.removing) {
		r.mu.Unlock()
		return nil, false
	}
	r.removing = true
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		r.removing = false
		r.mu.Unlock()
	}, true
}

// Reset forgets how the last install ended (the files it made are gone): the status is Idle again.
func (r *Runner) Reset() {
	r.mu.Lock()
	r.status = Status{}
	r.mu.Unlock()
}
