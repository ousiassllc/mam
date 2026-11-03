// Package apprestart is the use case behind "再起動して反映" (the settings screen) and the MCP tool restart_app: it checks what
// a restart would break, then asks the front to quit and start the app again (spec/architecture/restart.md).
//
// A restart is never made without being asked: Request is called by a person's click or by a tool call that says so. What it
// does when work is running is the caller's choice (WhenBusy): refuse, wait until the work is over, or stop (restart anyway).
// A download (a model, the ComfyUI engine, ffmpeg) is never cut by "stop": it is refused with the reason, so that a
// half-written file of tens of gigabytes is not left behind by a restart nobody waited for.
//
// The package does not import service, so it has its own errors; the front that really quits is behind Deps.Restart.
package apprestart

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/resilience"
	"github.com/ousiassllc/mam/backend/internal/service/restartpending"
)

// WhenBusy is what Request does when work is running.
type WhenBusy string

// The choices. The zero value is WhenBusyRefuse.
const (
	// WhenBusyRefuse restarts only when nothing is running; else it fails with a *BusyError and changes nothing.
	WhenBusyRefuse WhenBusy = "refuse"
	// WhenBusyWait returns at once and restarts when nothing is running any more (within the time given).
	WhenBusyWait WhenBusy = "wait"
	// WhenBusyStop restarts now: generation jobs that run fail ("interrupted by shutdown"), a job that can be resumed
	// is resumed by the next start. Not while a download runs (KindDownload): that is refused.
	WhenBusyStop WhenBusy = "stop"
)

// Valid reports whether w is one of the choices (the empty one included).
func (w WhenBusy) Valid() bool {
	return w == "" || w == WhenBusyRefuse || w == WhenBusyWait || w == WhenBusyStop
}

// The kinds of work that holds a restart back (Busy.Kind).
const (
	// KindJobs is a job of the queue that is running or queued (generate, judge, repair, plan, the generation test).
	KindJobs = "jobs"
	// KindEditing is an editing stage or the AUTO chain of a Project.
	KindEditing = "editing"
	// KindMedia is an ffmpeg / ffprobe process.
	KindMedia = "media"
	// KindWorkflow is a run of a saved ComfyUI workflow (the bundled ComfyUI is generating).
	KindWorkflow = "workflow_run"
	// KindDownload is an install of a model, the ComfyUI engine, a custom node or the local runtimes, or the download of
	// ffmpeg: the downloaded files are half written. "stop" does not cut it.
	KindDownload = "download"
	// KindCheckFailed is a lookup of what runs that failed: nothing is known, so nothing is assumed (fail closed).
	KindCheckFailed = "check_failed"
)

// Busy is one thing a restart would interrupt, in words for the user ("生成などのジョブ 2 件（実行中 1・待機 1）").
type Busy struct {
	Kind    string
	Message string
}

// BusyError is a request that was refused for the work that is running. Nothing was changed.
type BusyError struct {
	Busy []Busy
	// StopRefused is true when WhenBusyStop was asked and refused because of a download.
	StopRefused bool
}

func (e *BusyError) Error() string {
	var parts []string
	for _, b := range e.Busy {
		parts = append(parts, b.Message)
	}
	what := strings.Join(parts, "、")
	if e.StopRefused {
		return "ダウンロード・導入が動いているため、止めて再起動はしません（途中のファイルを残さないため）: " + what + "。終わるまで待つか（when_busy: wait）、導入をキャンセルしてください"
	}
	return "作業が動いているため再起動しません: " + what + "。終わるまで待つ（when_busy: wait）か、中断してよいなら when_busy: stop を指定してください"
}

// ErrUnavailable means the app has no way to restart itself (the browser build: no front owns the process).
var ErrUnavailable = errors.New("apprestart: this build cannot restart itself")

// State is where a request stands.
type State string

// The states of a Result.
const (
	// StateRestarting means the app was asked to quit and start again: this process ends within moments.
	StateRestarting State = "restarting"
	// StateWaiting means a restart will follow when nothing runs any more.
	StateWaiting State = "waiting"
)

// Result is the answer of Request.
type Result struct {
	State State
	// BootID is the id of this run of the app: after the restart, get_health shows another one.
	BootID string
	// Busy is what was running when the request came: what a "stop" interrupts, or what a "wait" waits for.
	Busy []Busy
	// WaitUntil is when a waiting request gives up (StateWaiting only).
	WaitUntil time.Time
}

// Options of Request.
type Options struct {
	WhenBusy WhenBusy
	// WaitFor bounds a WhenBusyWait; zero is DefaultWait, more than MaxWait is MaxWait.
	WaitFor time.Duration
}

// Wait bounds of a waiting request.
const (
	DefaultWait = 30 * time.Minute
	MaxWait     = 24 * time.Hour
)

// Deps are what the service works with.
type Deps struct {
	// Restart asks the front to quit and start the app again (desktop.Restarter). It must not block. Nil: the build cannot
	// restart itself (Available is false and Request is ErrUnavailable).
	Restart func()
	// Busy lists what is running now that a restart would interrupt, empty for none. It is asked at every request and at
	// every poll of a waiting one.
	Busy func(ctx context.Context) []Busy
	// Pending is the list of saved settings that wait for a restart (shown by Status).
	Pending *restartpending.Registry
	Logger  *slog.Logger
	// BootID names this run of the app; StartedAt is when it started.
	BootID    string
	StartedAt time.Time
	// PollEvery is how often a waiting request looks again; zero is 5 seconds. Sleep replaces the wait (tests).
	PollEvery time.Duration
	Sleep     func(ctx context.Context, d time.Duration) error
	Now       func() time.Time
}

// Service restarts the app on request. It is safe for concurrent use.
type Service struct {
	d      Deps
	ctx    context.Context
	cancel context.CancelFunc

	mu         sync.Mutex
	restarting bool
	waiting    bool
	waitUntil  time.Time
	wg         sync.WaitGroup
}

// New returns a Service. Close stops a waiting request.
func New(d Deps) *Service {
	if d.Logger == nil {
		d.Logger = slog.New(slog.DiscardHandler)
	}
	if d.PollEvery <= 0 {
		d.PollEvery = 5 * time.Second
	}
	if d.Sleep == nil {
		d.Sleep = resilience.SleepContext
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Busy == nil {
		d.Busy = func(context.Context) []Busy { return nil }
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{d: d, ctx: ctx, cancel: cancel}
}

// Available is whether this build can restart itself.
func (s *Service) Available() bool { return s.d.Restart != nil }

// Close ends a waiting request (it does not restart) and waits for its goroutine.
func (s *Service) Close() {
	s.cancel()
	s.wg.Wait()
}

// Status is what is known without asking for a restart.
type Status struct {
	Available bool
	BootID    string
	StartedAt time.Time
	// Pending are the saved settings that wait for a restart, in words for the user.
	Pending []string
	// Waiting is true while a request waits for the running work to end; WaitUntil is when it gives up.
	Waiting    bool
	WaitUntil  time.Time
	Restarting bool
}

// Status reports the run, the settings that wait for a restart and a waiting request.
func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Status{
		Available: s.Available(), BootID: s.d.BootID, StartedAt: s.d.StartedAt, Pending: s.d.Pending.Reasons(),
		Waiting: s.waiting, WaitUntil: s.waitUntil, Restarting: s.restarting,
	}
}

// Request restarts the app, or waits to, as o says. It returns once the app was asked to quit (StateRestarting; the process
// ends shortly after, so the answer may be the last one this run gives) or the wait began (StateWaiting). A refusal is a
// *BusyError, a build that cannot restart is ErrUnavailable; in both nothing was changed.
func (s *Service) Request(ctx context.Context, o Options) (Result, error) {
	if !s.Available() {
		return Result{}, ErrUnavailable
	}
	if !o.WhenBusy.Valid() {
		return Result{}, fmt.Errorf("apprestart: unknown when_busy %q", o.WhenBusy)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.restarting {
		return Result{State: StateRestarting, BootID: s.d.BootID}, nil
	}
	busy := s.d.Busy(ctx)
	res := Result{BootID: s.d.BootID, Busy: busy}
	switch {
	case len(busy) == 0:
		return s.trigger(res), nil
	case o.WhenBusy == WhenBusyWait:
		return s.startWaiting(res, o.WaitFor), nil
	case o.WhenBusy == WhenBusyStop:
		if hasKind(busy, KindDownload) {
			return Result{}, &BusyError{Busy: busy, StopRefused: true}
		}
		s.d.Logger.Warn("restart requested while work runs: it is interrupted", "busy", busyLog(busy))
		return s.trigger(res), nil
	}
	return Result{}, &BusyError{Busy: busy}
}

// trigger asks the front to quit. The caller holds mu.
func (s *Service) trigger(res Result) Result {
	s.restarting, s.waiting = true, false
	s.d.Logger.Info("restarting the app on request")
	s.d.Restart()
	res.State = StateRestarting
	return res
}

// startWaiting begins (or joins) a wait for the work to end. The caller holds mu.
func (s *Service) startWaiting(res Result, waitFor time.Duration) Result {
	res.State = StateWaiting
	if s.waiting {
		res.WaitUntil = s.waitUntil
		return res
	}
	switch {
	case waitFor <= 0:
		waitFor = DefaultWait
	case waitFor > MaxWait:
		waitFor = MaxWait
	}
	s.waiting, s.waitUntil = true, s.d.Now().Add(waitFor)
	res.WaitUntil = s.waitUntil
	s.wg.Add(1)
	go s.waitForIdle(waitFor)
	return res
}

// waitForIdle looks every PollEvery whether the work has ended and restarts when it has, or gives up after waitFor.
func (s *Service) waitForIdle(waitFor time.Duration) {
	defer s.wg.Done()
	ctx, cancel := context.WithTimeout(s.ctx, waitFor)
	defer cancel()
	for {
		if err := s.d.Sleep(ctx, s.d.PollEvery); err != nil {
			s.mu.Lock()
			s.waiting = false
			s.mu.Unlock()
			if errors.Is(err, context.DeadlineExceeded) {
				s.d.Logger.Warn("the waiting restart gave up: work was still running", "waited", waitFor)
			}
			return
		}
		s.mu.Lock()
		if s.restarting { // another request began the restart while this one waited
			s.mu.Unlock()
			return
		}
		if len(s.d.Busy(ctx)) == 0 {
			s.trigger(Result{})
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
	}
}

func hasKind(busy []Busy, kind string) bool {
	for _, b := range busy {
		if b.Kind == kind {
			return true
		}
	}
	return false
}

func busyLog(busy []Busy) string {
	kinds := make([]string, len(busy))
	for i, b := range busy {
		kinds[i] = b.Kind
	}
	return strings.Join(kinds, ",")
}
