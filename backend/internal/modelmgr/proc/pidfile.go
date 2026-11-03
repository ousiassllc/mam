//go:build !windows

package proc

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// errNoProcess is a pid that no process has (or a zombie, which has ended).
var errNoProcess = errors.New("no such process")

// errUnsupported is an OS that has no way to tell processes apart here (Windows, where a Job Object does the work).
var errUnsupported = errors.New("not supported on this OS")

// processID names one process in a way a reused pid does not match: its start time (opaque, from the OS) and its command line.
type processID struct {
	PID     int    `json:"pid"`
	Start   string `json:"start"`
	Cmdline string `json:"cmdline"`
}

// pidRecord is the pid file of a child: the child, and the mam that started it (a mam that is still running owns its child: another
// mam that uses the same directory must not end it).
type pidRecord struct {
	Child processID `json:"child"`
	Owner processID `json:"owner"`
}

// prober returns the processID of pid (errNoProcess when there is none). probeProcess is the one of this OS.
type prober func(pid int) (processID, error)

func (a processID) same(b processID) bool {
	return a.PID == b.PID && a.Start == b.Start && a.Cmdline == b.Cmdline
}

// writeRecord notes the child pid (and mam) in path, so that the next start of mam finds a child that outlived it. It returns false
// (nothing written) on an OS that cannot tell processes apart reliably, or when the file cannot be written.
func writeRecord(path string, child int, probe prober, logger *slog.Logger) bool {
	rec := pidRecord{}
	var err error
	if rec.Child, err = probe(child); err == nil {
		rec.Owner, err = probe(os.Getpid())
	}
	if err == nil {
		err = writeJSON(path, rec)
	}
	if errors.Is(err, errUnsupported) {
		return false
	}
	if err != nil {
		logger.Warn("pid file not written: a child that outlives mam is not found at the next start", "err", err)
	}
	return err == nil
}

func writeJSON(path string, rec pidRecord) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readRecord(path string) (pidRecord, error) {
	var rec pidRecord
	data, err := os.ReadFile(path) // #nosec G304 -- mam's own pid file
	if err != nil {
		return rec, err
	}
	return rec, json.Unmarshal(data, &rec)
}

// removeRecord deletes the pid file when it still names child (another mam of the same directory may have written its own).
func removeRecord(path string, child int) {
	if rec, err := readRecord(path); err == nil && rec.Child.PID == child {
		_ = os.Remove(path)
	}
}

// reclaim ends a child that a mam that is gone left behind, found by its pid file, and removes the file. Only a process that is
// the very one mam started is ended: the pid must have the start time and the command line that were written (a pid that the
// system gave to something else since is left alone), and the mam that wrote the file must be gone. It asks the group to end,
// then after grace kills it.
func reclaim(path string, probe prober, signal func(pid int, force bool), grace, poll time.Duration, logger *slog.Logger) {
	rec, err := readRecord(path)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		logger.Warn("pid file unreadable; removed", "err", err)
		_ = os.Remove(path)
		return
	}
	if owner, err := probe(rec.Owner.PID); err == nil && owner.same(rec.Owner) {
		return // that mam still runs and owns its child (and the file)
	}
	defer func() { _ = os.Remove(path) }()
	cur, err := probe(rec.Child.PID)
	if err != nil || rec.Child.PID <= 1 || rec.Child.PID == os.Getpid() || !cur.same(rec.Child) {
		logger.Debug("the child of the pid file is gone", "pid", rec.Child.PID, "probe_err", err)
		return
	}
	logger.Warn("ending a child that the previous mam left behind", "pid", rec.Child.PID)
	signal(rec.Child.PID, false)
	if !waitGone(rec.Child, probe, grace, poll) {
		signal(rec.Child.PID, true)
		waitGone(rec.Child, probe, grace, poll)
	}
}

// waitGone waits up to limit for the process to end (or to be somebody else's by then: another start time. The command line is not compared:
// the one of a process that is ending is empty).
func waitGone(id processID, probe prober, limit, poll time.Duration) bool {
	deadline := time.Now().Add(limit)
	for {
		if cur, err := probe(id.PID); err != nil || cur.Start != id.Start {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(poll)
	}
}
