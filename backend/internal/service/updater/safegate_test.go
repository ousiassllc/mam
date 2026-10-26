package updater_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/updater"
)

// fakeJobs is the job table: the jobs per status, or an error for one status.
type fakeJobs struct {
	byStatus map[domain.JobStatus]int
	failOn   domain.JobStatus
}

func (f fakeJobs) ListByStatus(_ context.Context, s domain.JobStatus) ([]domain.Job, error) {
	if s == f.failOn {
		return nil, errors.New("database is locked")
	}
	return make([]domain.Job, f.byStatus[s]), nil
}

func TestSafeGate_SafeToUpdate(t *testing.T) {
	tests := []struct {
		name    string
		jobs    fakeJobs
		editing bool
		media   int
		want    updater.BlockKind // empty: safe
	}{
		{name: "idle"},
		{name: "a running job", jobs: fakeJobs{byStatus: map[domain.JobStatus]int{domain.JobRunning: 1}}, want: updater.BlockJobs},
		{name: "a queued job", jobs: fakeJobs{byStatus: map[domain.JobStatus]int{domain.JobQueued: 2}}, want: updater.BlockJobs},
		{name: "finished jobs do not count", jobs: fakeJobs{byStatus: map[domain.JobStatus]int{domain.JobSucceeded: 5, domain.JobFailed: 1}}},
		{name: "an editing stage", editing: true, want: updater.BlockEditing},
		{name: "an ffmpeg process", media: 1, want: updater.BlockMedia},
		{name: "the running lookup fails", jobs: fakeJobs{failOn: domain.JobRunning}, want: updater.BlockCheckFailed},
		{name: "the queued lookup fails", jobs: fakeJobs{failOn: domain.JobQueued}, want: updater.BlockCheckFailed},
		{name: "jobs are reported before editing and ffmpeg", jobs: fakeJobs{byStatus: map[domain.JobStatus]int{domain.JobRunning: 1}}, editing: true, media: 3, want: updater.BlockJobs},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gate := updater.SafeGate{
				Jobs:         tt.jobs,
				EditingBusy:  func() bool { return tt.editing },
				MediaRunning: func() int { return tt.media },
			}
			safe, reason := gate.SafeToUpdate(context.Background())
			if safe != (tt.want == "") || reason.Kind != tt.want {
				t.Fatalf("SafeToUpdate = %v, %+v; want safe=%v kind=%q", safe, reason, tt.want == "", tt.want)
			}
			if !safe && reason.Detail == "" {
				t.Error("a refusal carries a Detail for the log")
			}
		})
	}
}
