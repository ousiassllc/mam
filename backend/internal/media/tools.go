package media

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/ousiassllc/mam/backend/internal/media/locate"
)

// Tools is the ffmpeg and ffprobe executables. Copies of a Tools share what it holds, so that after Reload every service that was handed
// the Tools at startup runs the ffmpeg set or downloaded later, with no restart. A call reads what the Tools holds once, when it
// starts: a Reload never changes an ffmpeg that is running. The zero Tools holds no executables: every call returns an error
// wrapping ErrUnavailable.
type Tools struct {
	live *live // nil in the zero Tools, which cannot Reload
}

// live is what the copies of a Tools share.
type live struct {
	mu  sync.Mutex // serializes Reload: a slower look never overwrites a newer one
	cur atomic.Pointer[exes]
}

// exes are the executables a Tools holds at one moment.
type exes struct {
	ffmpeg, ffprobe string
	lim             limits // the zero value means defaultLimits
}

var noExes = &exes{}

// NewTools is a Tools that holds nothing until Reload: the app hands it to the services before it knows that ffmpeg is there.
func NewTools() Tools { return Tools{live: &live{}} }

func newTools(e exes) Tools {
	t := NewTools()
	t.live.cur.Store(&e)
	return t
}

// At is a Tools that holds the given executables (a test's fake, or a path that is known): it can still be changed by Reload.
func At(ffmpeg, ffprobe string) Tools {
	return newTools(exes{ffmpeg: ffmpeg, ffprobe: ffprobe, lim: defaultLimits})
}

// Reload makes t, and every copy of it, run what look finds from now on. An error from look (ffmpeg is not found) makes t hold nothing:
// every later call fails with ErrUnavailable, and the error is returned. A call that has started keeps the ffmpeg it started with.
// t MUST come from NewTools.
func (t Tools) Reload(look func() (Tools, error)) error {
	t.live.mu.Lock()
	defer t.live.mu.Unlock()
	next, err := look()
	t.live.cur.Store(next.exes())
	return err
}

// exes is what t holds now.
func (t Tools) exes() *exes {
	if t.live == nil {
		return noExes
	}
	if e := t.live.cur.Load(); e != nil {
		return e
	}
	return noExes
}

// Available says whether t holds both executables: a nil Tools and the zero Tools do not.
func (t *Tools) Available() bool {
	if t == nil {
		return false
	}
	_, err := t.ready()
	return err == nil
}

// ready is what t holds now, for one call to run on; ErrUnavailable when it lacks either executable.
func (t Tools) ready() (*exes, error) {
	e := t.exes()
	if e.ffmpeg == "" || e.ffprobe == "" {
		return nil, ErrUnavailable
	}
	return e, nil
}

// FFmpegPath is the ffmpeg executable found ("" when there is none): what a local runtime, which ships none, is told to run.
func (t Tools) FFmpegPath() string { return t.exes().ffmpeg }

// FFprobePath is the ffprobe executable found ("" when there is none).
func (t Tools) FFprobePath() string { return t.exes().ffprobe }

// Look finds ffmpeg and ffprobe with no path set by the user: the search of locate.Find (PATH, then the Homebrew
// directories on macOS). It returns an error wrapping ErrUnavailable when either is missing.
func Look() (Tools, error) { return LookWith(locate.Config{}) }

// LookWith finds ffmpeg and ffprobe on this machine: the path in cfg when it is set, else the search (LookIn of the
// machine's Finder).
func LookWith(cfg locate.Config) (Tools, error) { return LookIn(locate.DefaultFinder(), cfg) }

// LookIn finds ffmpeg and ffprobe with f. It returns an error wrapping ErrUnavailable that says, for each of the two that
// is missing, where it looked or which configured path cannot be used (errors.As finds a *locate.PathError).
func LookIn(f locate.Finder, cfg locate.Config) (Tools, error) {
	ffmpeg, err1 := f.Find(locate.FFmpeg, cfg.FFmpeg)
	ffprobe, err2 := f.Find(locate.FFprobe, cfg.FFprobe)
	if err := errors.Join(err1, err2); err != nil {
		return Tools{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return newTools(exes{ffmpeg: ffmpeg, ffprobe: ffprobe, lim: defaultLimits}), nil
}
