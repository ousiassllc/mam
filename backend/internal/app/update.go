package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/appupdate"
	"github.com/ousiassllc/mam/backend/internal/service/updater"
	"github.com/ousiassllc/mam/backend/internal/service/updater/tempcleanup"
	"github.com/ousiassllc/mam/backend/internal/version"
)

// UpdateOptions turns the automatic update on. Only the Wails front sets it: the headless server has no
// installer to run, so it never checks (spec/architecture/desktop.md §Auto update).
type UpdateOptions struct {
	// Install is called once, with the path of a verified installer, when a newer release was
	// downloaded and no work is in flight. It must make the application quit and run the installer
	// after the server stopped. It must not block.
	Install func(installerPath string)
	// Schedule is when to check; the zero value is the first check a minute after the start, then every
	// 6 hours (a held-back or failed check is repeated sooner).
	Schedule appupdate.Schedule
}

// startUpdates builds the update service, removes the installers a former update left in the temp
// directory and starts the schedule. stop ends it; the caller runs it before it stops the services.
func startUpdates(ctx context.Context, o *UpdateOptions, services *service.Services, jobs *repository.JobRepository, logger *slog.Logger) (_ *appupdate.Service, stop func()) {
	if removed, err := tempcleanup.CleanupStale(time.Now()); err != nil {
		logger.Warn("clean up stale update downloads failed", "removed", removed, "err", err)
	} else if removed > 0 {
		logger.Info("removed stale update downloads", "removed", removed)
	}
	checker := updater.NewChecker(updater.Config{
		Owner:  version.GitHubOwner,
		Repo:   version.GitHubRepo,
		Gate:   safeGate(services, jobs),
		Logger: logger,
	})
	svc := appupdate.New(checker, version.Version, o.Install, logger)
	return svc, svc.Start(ctx, o.Schedule)
}

// safeGate is the gate of the updater over what really runs in this process: the job table for the
// background jobs (it is written before a job runs and when it ends, so it also sees a job no worker has
// claimed yet), the editing stages and the AUTO chain of EditorService, and the ffmpeg processes of media.
func safeGate(services *service.Services, jobs updater.JobLister) updater.SafeGate {
	return updater.SafeGate{Jobs: jobs, EditingBusy: services.Editor.Busy, MediaRunning: media.Running}
}
