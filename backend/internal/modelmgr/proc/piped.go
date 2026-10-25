package proc

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"
)

// childPipes are the two pipes between mam and a child that speaks over its standard input and output. The child's
// ends are handed to the process and closed in mam once it has started; the other ends are the caller's. They are
// files, not exec's own pipes, so that Wait does not close the output before the caller has read all of it.
type childPipes struct {
	childIn, childOut *os.File // the child's stdin (read end) and stdout (write end)
	in                *os.File // mam writes the child's stdin here
	out               *os.File // mam reads the child's stdout here
}

func newChildPipes() (*childPipes, error) {
	inR, inW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		_ = inR.Close()
		_ = inW.Close()
		return nil, err
	}
	return &childPipes{childIn: inR, childOut: outW, in: inW, out: outR}, nil
}

func (p *childPipes) closeChildEnds() {
	if p != nil {
		_ = p.childIn.Close()
		_ = p.childOut.Close()
	}
}

func (p *childPipes) closeAll() {
	p.closeChildEnds()
	_ = p.in.Close()
	_ = p.out.Close()
}

// Piped is a child whose standard input and output are piped to the caller (the Codex app-server speaks JSON-RPC over
// them). It has the rules of the other children of this package: no console window, a group that the system ends with
// mam, and its standard error is kept only as the tail in memory (and in Spec.LogPath when set).
type Piped struct {
	// Stdin is the child's standard input; closing it is the polite end of an app-server.
	Stdin io.WriteCloser
	// Stdout is the child's standard output; it ends (EOF) when the child has exited. The caller reads it to the end
	// and closes it.
	Stdout io.ReadCloser
	r      *run
	pipes  *childPipes
}

// StartPiped starts spec with its standard input and output piped to the caller.
func StartPiped(spec Spec, logger *slog.Logger) (*Piped, error) {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	pipes, err := newChildPipes()
	if err != nil {
		return nil, fmt.Errorf("create pipes: %w", err)
	}
	r, err := launch(spec, logger, pipes)
	if err != nil {
		pipes.closeAll()
		return nil, err
	}
	return &Piped{Stdin: pipes.in, Stdout: pipes.out, r: r, pipes: pipes}, nil
}

// Done is closed when the child has exited.
func (p *Piped) Done() <-chan struct{} { return p.r.done }

// ExitCode is the exit code of the child once Done is closed (-1 for a child that was killed or could not be waited for).
func (p *Piped) ExitCode() int { return p.r.exitCode() }

// Stopping reports whether Stop was called: an exit after it is not a crash.
func (p *Piped) Stopping() bool { return p.r.stopping() }

// Stop ends the child: its standard input is closed first, then (after timeout, per step) the group is interrupted and killed.
func (p *Piped) Stop(timeout time.Duration) {
	p.r.markStopping()
	_ = p.pipes.in.Close()
	select {
	case <-p.r.done:
	case <-time.After(timeout):
		p.r.terminate(timeout)
	}
}

// LogTail is the end of what the child wrote to its standard error ("" when nothing), for a failure message.
func (p *Piped) LogTail() string { return p.r.logTail() }
