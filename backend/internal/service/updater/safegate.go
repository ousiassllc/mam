package updater

import (
	"context"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Gate decides whether the running app may be restarted now. Checker asks it before the download
// and again right after it, because the download takes minutes and the caller quits and installs as
// soon as the Result is Ready.
type Gate interface {
	SafeToUpdate(ctx context.Context) (bool, BlockReason)
}

// BlockKind names the work that held an installation back, so that the settings screen can say
// what to wait for. The zero value means "not blocked".
type BlockKind string

const (
	// BlockJobs means a background job (generate, judge, repair, plan) is queued or running.
	BlockJobs BlockKind = "jobs"
	// BlockEditing means an editing stage (Rough Cut, Editor Judge, audio, Final QC, export) or the AUTO
	// chain of a Project is running.
	BlockEditing BlockKind = "editing"
	// BlockMedia means an ffmpeg / ffprobe process is running (a preview, Partial Take Use, a conversion).
	BlockMedia BlockKind = "media"
	// BlockCheckFailed means a lookup of the gate failed, so safety could not be established (fail closed).
	BlockCheckFailed BlockKind = "check_failed"
)

// BlockReason is why SafeToUpdate refused: Kind for the screen, Detail (English, with counts) for the log.
type BlockReason struct {
	Kind   BlockKind
	Detail string
}

// JobLister is the job table. *repository.JobRepository implements it. The table is the authority
// for the background jobs: every job is stored before it runs and its status is written when it
// starts and ends, so it also sees a job the in-memory queue has not claimed yet.
type JobLister interface {
	ListByStatus(ctx context.Context, status domain.JobStatus) ([]domain.Job, error)
}

// SafeGate is the gate of the app: nothing a restart would lose or corrupt is in flight. A restart
// fails a running generate / repair job (it is not resumable) and cuts short an ffmpeg run, an editing
// stage and the AUTO chain; a queued job survives it but would start right after the restart anyway,
// so it counts as work in flight too. Jobs come from the job table (JobLister); the editing stages and
// ffmpeg exist only in memory, so they come from the registries that run them.
type SafeGate struct {
	Jobs JobLister
	// EditingBusy reports whether an editing stage or an AUTO chain of any Project is running
	// (service.EditorService.Busy).
	EditingBusy func() bool
	// MediaRunning reports how many ffmpeg / ffprobe processes are running (media.Running).
	MediaRunning func() int
}

// SafeToUpdate reports whether nothing is in flight and, when something is, the first thing found
// (checked in the order: jobs, editing, ffmpeg). A failed lookup is a refusal.
func (g SafeGate) SafeToUpdate(ctx context.Context) (bool, BlockReason) {
	inFlight := 0
	for _, status := range []domain.JobStatus{domain.JobRunning, domain.JobQueued} {
		jobs, err := g.Jobs.ListByStatus(ctx, status)
		if err != nil {
			return false, BlockReason{BlockCheckFailed, fmt.Sprintf("%s job lookup failed: %v", status, err)}
		}
		inFlight += len(jobs)
	}
	if inFlight > 0 {
		return false, BlockReason{BlockJobs, fmt.Sprintf("background jobs in flight: count=%d", inFlight)}
	}
	if g.EditingBusy() {
		return false, BlockReason{BlockEditing, "an editing stage or the AUTO chain is running"}
	}
	if n := g.MediaRunning(); n > 0 {
		return false, BlockReason{BlockMedia, fmt.Sprintf("ffmpeg processes running: count=%d", n)}
	}
	return true, BlockReason{}
}
