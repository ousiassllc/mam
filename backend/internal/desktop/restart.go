package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// restartWaitEnv carries, to the process that restarts the app, the pid of the process it replaces: the new process waits
// for that one to end before it does anything (WaitForPredecessor), because the old one still holds the single-instance
// lock of the window and the data directory until it has quit.
const restartWaitEnv = "MAM_RESTART_WAIT_PID"

// restartWaitLimit is how long the new process waits for the old one to end; the old one stops the server and the local
// engines first, which takes seconds. A new process that waited this long starts anyway (the single-instance check then
// decides: it brings the old window forward and ends).
const restartWaitLimit = 60 * time.Second

// macRestartScript is the helper the macOS restart runs in a process of its own: /bin/sh -c <script> mam-restart <pid>
// <command> [args...]. It waits for process <pid> (the application that is quitting) to end, at most a minute, then runs
// the command (open of the bundle, or the executable itself outside a bundle). The pid and the command come in as
// arguments, never spliced into the script. POSIX sh: /bin/sh of macOS is bash 3.2 in sh mode.
const macRestartScript = `
pid=$1
shift
n=0
while kill -0 "$pid" 2>/dev/null; do
  n=$((n + 1))
  [ "$n" -gt 600 ] && break
  sleep 0.1
done
exec "$@"
`

// RelaunchCommand builds, without starting it, the command that starts the application again once the process pid (this
// one) has ended, with the arguments it was started with. It is a function of its own because the restart cannot be run
// on the Linux CI: what is tested is the command that is built.
//
// On macOS a bundle is started through open (LaunchServices: the Dock icon and the window come as for a launch by hand),
// after a shell helper has waited for pid; outside a bundle the executable itself is run by the same helper. Elsewhere
// (Windows) the executable is started at once with restartWaitEnv set, and waits for pid itself (WaitForPredecessor).
// environ is the environment of this process.
func RelaunchCommand(goos string, pid int, exe string, args, environ []string) *exec.Cmd {
	if goos == "darwin" {
		command := []string{exe}
		if bundle, err := MacAppBundle(exe); err == nil {
			command = []string{"/usr/bin/open", bundle}
			if len(args) > 0 {
				command = append(command, "--args")
			}
		}
		argv := append([]string{"-c", macRestartScript, "mam-restart", strconv.Itoa(pid)}, command...)
		cmd := exec.Command("/bin/sh", append(argv, args...)...) // #nosec G204 G702 -- fixed script; this app's own executable and arguments
		detach(cmd)
		return cmd
	}
	cmd := exec.Command(exe, args...) // #nosec G204 G702 -- this app's own executable and arguments
	cmd.Env = append(withoutEnv(environ, restartWaitEnv), restartWaitEnv+"="+strconv.Itoa(pid))
	detach(cmd)
	return cmd
}

// withoutEnv is environ without the variable name.
func withoutEnv(environ []string, name string) []string {
	out := make([]string, 0, len(environ))
	for _, kv := range environ {
		if !strings.HasPrefix(kv, name+"=") {
			out = append(out, kv)
		}
	}
	return out
}

// WaitForPredecessor makes a process that was started by a restart wait for the process it replaces: when restartWaitEnv
// names a pid it waits for that process to end (at most restartWaitLimit) and clears the variable (so that nothing this
// process starts inherits it). It does nothing for an ordinary start. Call it first in main, before the single-instance
// check.
func WaitForPredecessor() {
	v := os.Getenv(restartWaitEnv)
	if v == "" {
		return
	}
	_ = os.Unsetenv(restartWaitEnv)
	if pid, err := strconv.Atoi(v); err == nil && pid > 0 {
		waitForExit(pid, restartWaitLimit)
	}
}

// Restarter is how the desktop front quits for a restart that was asked for (the settings screen, the MCP tool) and starts
// the application again once the server has stopped. It is the sibling of Updater: Restart asks the application to quit, the
// front stops the server and then calls RunPending.
type Restarter struct {
	quit    func()
	start   func(*exec.Cmd) error
	command func() (*exec.Cmd, error)

	mu        sync.Mutex
	requested bool
}

// NewRestarter returns a Restarter that asks the application to quit with quit (which must make the OnShutdown hook, and so
// RunPending, run).
func NewRestarter(quit func()) *Restarter {
	return &Restarter{quit: quit, start: (*exec.Cmd).Start, command: runningRelaunch}
}

// runningRelaunch is RelaunchCommand for this process.
func runningRelaunch() (*exec.Cmd, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("locate the running executable: %w", err)
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return nil, fmt.Errorf("resolve the running executable: %w", err)
	}
	return RelaunchCommand(runtime.GOOS, os.Getpid(), exe, os.Args[1:], os.Environ()), nil
}

// Restart remembers that the application is to start again and quits it. It does not block: the caller may be a request the
// shutdown waits for, so quit runs on a goroutine of its own.
func (r *Restarter) Restart() {
	r.mu.Lock()
	r.requested = true
	r.mu.Unlock()
	go r.quit()
}

// Requested reports whether Restart was called.
func (r *Restarter) Requested() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.requested
}

// RunPending starts the application again, detached from this process, when Restart was called, and returns the error of
// starting it. It does nothing (nil) otherwise. Call it after the server stopped: the new process waits for this one to end.
func (r *Restarter) RunPending() error {
	if !r.Requested() {
		return nil
	}
	cmd, err := r.command()
	if err != nil {
		return err
	}
	return r.start(cmd)
}
