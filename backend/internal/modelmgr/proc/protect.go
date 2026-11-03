//go:build !windows

package proc

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"
)

// The protection of a child from a mam that ends without Close (model-manager.md §3.4): the guard process on macOS, and a pid file that the
// next start of mam reads. Windows has a Job Object and needs neither (protect_windows.go).

// guardAndNote protects the started child: the guard process, and the pid file when the Spec names one. Neither failing stops the child.
func (r *run) guardAndNote(pidFile string, logger *slog.Logger) {
	if err := startGuard(r.cmd.Process.Pid); err != nil {
		logger.Warn("guard process not started: a child outlives a mam that is killed", "err", err)
	}
	if pidFile != "" && writeRecord(pidFile, r.cmd.Process.Pid, probeProcess, logger) {
		pid := r.cmd.Process.Pid
		r.forget = func() { removeRecord(pidFile, pid) } // the child has ended: its pid file goes
	}
}

// reclaimLeftover ends the child that a mam which is gone left behind (Config.PIDFile), before the next one starts.
func reclaimLeftover(cfg Config) {
	if cfg.PIDFile != "" {
		reclaim(cfg.PIDFile, probeProcess, signalLeftover, cfg.StopTimeout, 50*time.Millisecond, cfg.Logger)
	}
}

// RunGuardIfRequested runs the guard when this process was started as one (guardFlag) and reports whether it was: main calls it
// first and returns when it is true. It does nothing (false) for every other start.
func RunGuardIfRequested() bool {
	if len(os.Args) != 4 || os.Args[1] != guardFlag {
		return false
	}
	parent, err1 := strconv.Atoi(os.Args[2])
	child, err2 := strconv.Atoi(os.Args[3])
	if err := errors.Join(err1, err2); err != nil || parent <= 1 || child <= 1 {
		fmt.Fprintln(os.Stderr, "mam: bad guard arguments")
		return true
	}
	if err := runGuard(parent, child); err != nil {
		fmt.Fprintln(os.Stderr, "mam:", err)
	}
	return true
}
