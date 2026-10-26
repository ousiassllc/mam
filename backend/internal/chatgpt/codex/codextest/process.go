package codextest

import (
	"io"
	"sync"
)

// Process is the in-memory process of a Fake: it satisfies appserver.Process.
type Process struct {
	f        *Fake
	stdin    *io.PipeWriter // written by the client
	stdout   *io.PipeReader // read by the client
	srvIn    *io.PipeReader
	srvOut   *exitPipe
	done     chan struct{}
	stopOnce sync.Once
	mu       sync.Mutex
	info     string
}

// exitPipe is the server's output; closing it with an exit code ends the process.
type exitPipe struct {
	w *io.PipeWriter
	p *Process
}

func (e *exitPipe) Write(b []byte) (int, error) { return e.w.Write(b) }

// CloseWithExit ends the process as a crash with the exit code.
func (e *exitPipe) CloseWithExit(code int) {
	e.p.mu.Lock()
	e.p.info = exitInfo(code)
	e.p.mu.Unlock()
	_ = e.w.Close()
	_ = e.p.srvIn.Close()
	e.p.markDone()
}

func exitInfo(code int) string {
	return "Codex app-server が終了しました（終了コード " + itoa(code) + "）"
}

func (p *Process) markDone() {
	p.stopOnce.Do(func() { close(p.done) })
}

// Process starts a new in-memory process served by the Fake.
func (f *Fake) Process() *Process {
	cliOutR, srvOutW := io.Pipe()
	srvInR, cliInW := io.Pipe()
	p := &Process{f: f, stdin: cliInW, stdout: cliOutR, srvIn: srvInR, done: make(chan struct{})}
	p.srvOut = &exitPipe{w: srvOutW, p: p}
	fp := f.attach(srvInR, p.srvOut)
	go func() {
		fp.serve()
		// The client closed its input (the polite stop), or the stream broke: the process ends.
		_ = srvOutW.Close()
		p.markDone()
	}()
	return p
}

// Stdin implements appserver.Process.
func (p *Process) Stdin() io.Writer { return p.stdin }

// Stdout implements appserver.Process.
func (p *Process) Stdout() io.Reader { return p.stdout }

// Done implements appserver.Process.
func (p *Process) Done() <-chan struct{} { return p.done }

// ExitInfo implements appserver.Process.
func (p *Process) ExitInfo() string {
	select {
	case <-p.done:
	default:
		return ""
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.info
}

// Stop implements appserver.Process: the standard input is closed and the process ends.
func (p *Process) Stop() {
	_ = p.stdin.Close()
	_ = p.srvIn.Close()
	p.markDone()
}
