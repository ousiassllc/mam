package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync/atomic"
	"time"
)

// ErrTimeout means ffmpeg or ffprobe ran past its time limit and was killed: a broken file or an
// unreadable network mount can make it hang, which would hold a job worker, the preview lock or a
// request for good.
var ErrTimeout = errors.New("media: ffmpeg timed out")

// waitDelay is how long a killed process may keep its output pipes open before they are closed on
// it (a grandchild that inherited them would otherwise keep the call waiting).
const waitDelay = 5 * time.Second

// limits are the time limits of the ffmpeg and ffprobe runs. A probe or a single frame is short;
// an encode (and a decode of a whole file) takes a time that grows with the length of the media:
// writeBase + writePerSecond for every second of it.
type limits struct {
	probe          time.Duration
	loudness       time.Duration
	writeBase      time.Duration
	writePerSecond time.Duration
}

var defaultLimits = limits{
	probe:          30 * time.Second,
	loudness:       10 * time.Minute,
	writeBase:      2 * time.Minute,
	writePerSecond: 5 * time.Second,
}

// limits returns the time limits of the tools; the zero Tools gets the defaults.
func (t Tools) limits() limits {
	if t.lim == (limits{}) {
		return defaultLimits
	}
	return t.lim
}

// writeLimit is the time limit of an ffmpeg run on seconds of media.
func (l limits) writeLimit(seconds float64) time.Duration {
	return l.writeBase + time.Duration(max(seconds, 0)*float64(l.writePerSecond))
}

// commandContext builds the exec.Cmd of every ffmpeg / ffprobe run. It is a variable so that a test
// can observe the command run builds (the Windows test checks it hides its console window).
var commandContext = exec.CommandContext

// active is the number of ffmpeg / ffprobe processes that are running.
var active atomic.Int32

// Running is how many ffmpeg / ffprobe processes (of any Tools) are running now. The app updater
// does not restart the app while there is one: it would be killed half way.
func Running() int { return int(active.Load()) }

// run executes exe with args, killing it after limit, and returns what it printed. A cancelled ctx
// returns ctx.Err(); the limit running out returns ErrTimeout.
func run(ctx context.Context, limit time.Duration, exe string, args ...string) (stdout, stderr []byte, err error) {
	runCtx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	var out, errOut bytes.Buffer
	cmd := commandContext(runCtx, exe, args...) // #nosec G204 -- arguments are built by this package; paths are asset files
	hideWindow(cmd)
	cmd.Stdout, cmd.Stderr = &out, &errOut
	cmd.WaitDelay = waitDelay
	active.Add(1)
	err = cmd.Run()
	active.Add(-1)
	if err != nil {
		switch {
		case ctx.Err() != nil:
			err = ctx.Err()
		case runCtx.Err() != nil:
			err = fmt.Errorf("%w after %s", ErrTimeout, limit)
		}
		return out.Bytes(), errOut.Bytes(), err
	}
	return out.Bytes(), errOut.Bytes(), nil
}
