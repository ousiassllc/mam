// Package appupdate is what the rest of the app knows about the automatic update of the Windows desktop
// build (spec/architecture/desktop.md §Auto update): the state to show on the settings screen, the
// manual check, and the schedule that checks at startup and every few hours. It wraps the
// updater package (the GitHub access, the verification and the safety gate), so that the web layer
// never imports that one, and it does not know how the app quits and runs the installer: the front
// hands it that as a function.
//
// Only the Wails front builds a Service. The headless server has none (a nil *Service is "no
// updater": the settings screen then shows the version only).
package appupdate

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"

	"github.com/ousiassllc/mam/backend/internal/service/updater"
)

// Checker is *updater.Checker.
type Checker interface {
	CheckForUpdate(ctx context.Context) (updater.Result, error)
	Status() updater.Status
}

// Service runs the update checks and tells their state.
type Service struct {
	checker Checker
	version string
	install func(installerPath string)
	logger  *slog.Logger

	checking   atomic.Int32 // checks running or waiting for the Checker
	installing atomic.Bool  // a verified installer was handed to install: no further check
}

// New returns a Service over checker. version is what is running (version.Version). install is called
// once, with the path of a verified installer, when a check finds a newer release and the safety
// gate lets it in: it must make the app quit and run the installer, and it must not block (the
// check that calls it is waited for by the quit).
func New(checker Checker, version string, install func(installerPath string), logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Service{checker: checker, version: version, install: install, logger: logger}
}

// View is the state of the update for the settings screen.
func (s *Service) View() View {
	return viewOf(s.version, s.checker.Status(), s.checking.Load() > 0)
}

// CheckNow runs a check now (POST /settings/update/check) and returns the state it ended in. The error
// is the failure of the check, which the View already shows as StateError.
func (s *Service) CheckNow(ctx context.Context) (View, error) {
	err := s.Check(ctx)
	return s.View(), err
}

// Check runs one check: look for a newer release and, when one is there and no work is in flight,
// download and verify it and hand it to install. A check does nothing once an installer was handed over.
//
// A failed check is logged here, at ERROR, with its kind (and the newer version when one is known): this
// is the one place both callers (the schedule and CheckNow, the manual check) go through, so each failure
// is logged exactly once and the schedule only logs what it decides to do about it. "No release" is not a
// failure, and a check that ctx cancelled (the app is quitting, the request went away) is not logged.
func (s *Service) Check(ctx context.Context) error {
	if s.installing.Load() {
		return nil
	}
	s.checking.Add(1)
	defer s.checking.Add(-1)
	result, err := s.checker.CheckForUpdate(ctx)
	if err != nil {
		if ctx.Err() == nil {
			st := s.checker.Status()
			s.logger.Error("update: check failed", "kind", st.ErrorKind, "version", st.Version, "err", err)
		}
		return err
	}
	if result.Ready && s.installing.CompareAndSwap(false, true) {
		s.logger.Info("update: installing the new version", "version", result.Version)
		s.install(result.InstallerPath)
	}
	return nil
}

// pending reports whether the last check found a newer release that the safety gate holds back.
func (s *Service) pending() bool {
	st := s.checker.Status()
	return st.Blocked && !st.Ready
}

// retryable reports whether the failed check err is worth repeating soon: not the failures that only
// another release can fix (the updater marks them Permanent), and not a cancelled check.
func retryable(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	var perm interface{ Permanent() bool }
	if errors.As(err, &perm) && perm.Permanent() {
		return false
	}
	return true
}
