package proc

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	tailBytes   = 4096
	maxLogBytes = 5 << 20
)

// run is one launched child.
type run struct {
	cmd  *exec.Cmd
	grp  *group
	tail *tail
	log  *os.File
	done chan struct{}
	code atomic.Int32
	stop atomic.Bool
}

func (r *run) pid() int {
	if r.cmd.Process == nil {
		return 0
	}
	return r.cmd.Process.Pid
}
func (r *run) exitCode() int  { return int(r.code.Load()) }
func (r *run) stopping() bool { return r.stop.Load() }
func (r *run) markStopping()  { r.stop.Store(true) }

// logTail is the end of the output of the child for a failure message ("" when it printed nothing).
func (r *run) logTail() string {
	t := strings.TrimSpace(r.tail.String())
	if t == "" {
		return ""
	}
	return "\n--- エンジンのログの末尾 ---\n" + t
}

// launch starts the child in a group that is killed with mam, and returns once it has started.
func launch(spec Spec, logger *slog.Logger) (*run, error) {
	r := &run{tail: &tail{}, done: make(chan struct{})}
	var out io.Writer = r.tail
	if spec.LogPath != "" {
		f, err := openLog(spec.LogPath)
		if err != nil {
			return nil, err
		}
		r.log, out = f, io.MultiWriter(r.tail, f)
	}
	cmd := exec.Command(spec.Command, spec.Args...) // #nosec G204 -- the command is the engine the catalog pinned, in mam's own directory
	cmd.Dir, cmd.Stdout, cmd.Stderr = spec.Dir, out, out
	cmd.Env = append(os.Environ(), spec.Env...)
	cmd.WaitDelay = 2 * time.Second // a grandchild that keeps the pipe open must not hold Wait
	prepare(cmd)
	grp, err := newGroup()
	if err != nil {
		r.closeLog()
		return nil, fmt.Errorf("create process group: %w", err)
	}
	r.cmd, r.grp = cmd, grp

	started := make(chan error, 1)
	go func() {
		if runtime.GOOS == "linux" {
			// Pdeathsig fires when the thread that started the child ends: keep this goroutine on its thread
			// until the child has ended.
			runtime.LockOSThread()
		}
		err := cmd.Start()
		if err == nil {
			err = grp.add(cmd.Process)
			if err != nil {
				_ = cmd.Process.Kill()
			}
		}
		started <- err
		if err != nil && cmd.Process == nil {
			r.finish(-1)
			return
		}
		werr := cmd.Wait()
		code := cmd.ProcessState.ExitCode()
		if werr != nil && code == 0 {
			code = -1
		}
		r.finish(code)
	}()
	if err := <-started; err != nil {
		<-r.done
		return nil, err
	}
	logger.Info("comfyui started", "pid", r.pid(), "dir", filepath.Base(spec.Dir))
	return r, nil
}

func (r *run) finish(code int) {
	r.code.Store(int32(code)) //nolint:gosec // an exit code
	r.grp.kill()              // anything the child left behind
	r.grp.close()
	r.closeLog()
	close(r.done)
}

func (r *run) closeLog() {
	if r.log != nil {
		_ = r.log.Close()
	}
}

// terminate stops the child: politely first where the OS has a way, then for good after timeout.
func (r *run) terminate(timeout time.Duration) {
	r.grp.interrupt()
	select {
	case <-r.done:
		return
	case <-time.After(timeout):
	}
	r.grp.kill()
	select {
	case <-r.done:
	case <-time.After(timeout):
	}
}

// openLog opens the log for appending; a log of more than 5 MiB starts anew.
func openLog(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if st, err := os.Stat(path); err == nil && st.Size() > maxLogBytes {
		flags = os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return os.OpenFile(path, flags, 0o600) // #nosec G304 -- mam's own log path
}

// tail keeps the last bytes written to it.
type tail struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > tailBytes {
		t.buf = append([]byte(nil), t.buf[len(t.buf)-tailBytes:]...)
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.ToValidUTF8(string(t.buf), "")
}
