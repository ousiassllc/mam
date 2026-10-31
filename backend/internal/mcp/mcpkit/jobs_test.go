package mcpkit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/jobhistory"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/service"
)

type fakeTable map[string]domain.Job

func (f fakeTable) Get(_ context.Context, id string) (domain.Job, error) {
	if j, ok := f[id]; ok {
		return j, nil
	}
	return domain.Job{}, repository.ErrNotFound
}

// shotWork is the persisted state a Probe reads in these tests.
type shotWork struct {
	state string
	takes []string
}

func (w *shotWork) probe(_ context.Context, job JobInfo) (Progress, error) {
	p := Progress{Done: len(w.takes), Total: 2, Result: map[string]any{"shot_id": job.Target}}
	switch w.state {
	case "running":
		p.State, p.Message = JobRunning, "generating"
	case "done":
		p.State, p.Result["take_ids"] = JobSucceeded, w.takes
	default:
		p.State, p.Error = JobFailed, "no take"
	}
	return p, nil
}

// newJobs makes Jobs over a temporary database on a clock the test moves. The history reads the same clock,
// so that its retention is not a function of the day the test runs.
func newJobs(t *testing.T, table JobTable) (*Jobs, *time.Time) {
	t.Helper()
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	j := NewJobs(table, jobhistory.New(testdb.Open(t)).WithClock(clock))
	j.now, j.bootedAt = clock, now
	return j, &now
}

func TestJobs_FollowTheWorkFromRunningToSucceeded(t *testing.T) {
	ctx := context.Background()
	j, clock := newJobs(t, nil)
	work := &shotWork{state: "running"}
	j.RegisterKind("generate_shot", work.probe)

	var startedBefore time.Time
	ref, err := j.Run(ctx, "generate_shot", "shot-1", func(context.Context) (string, error) {
		startedBefore = *clock
		return "", nil
	})
	require.NoError(t, err)
	assert.Equal(t, JobQueued, ref.State)
	assert.Contains(t, ref.JobID, "job_")

	st, err := j.Get(ctx, ref.JobID)
	require.NoError(t, err)
	assert.Equal(t, JobStatus{
		JobID: ref.JobID, Kind: "generate_shot", Target: "shot-1", State: JobRunning, Message: "generating",
		Done: 0, Total: 2, Result: map[string]any{"shot_id": "shot-1"}, StartedAt: startedBefore,
	}, st)

	again, err := j.Get(ctx, ref.JobID)
	require.NoError(t, err)
	assert.Equal(t, st, again, "asking again changes nothing")

	work.state, work.takes = "done", []string{"take-1", "take-2"}
	st, err = j.Get(ctx, ref.JobID)
	require.NoError(t, err)
	assert.Equal(t, JobSucceeded, st.State)
	assert.Equal(t, []string{"take-1", "take-2"}, st.Result["take_ids"])
	assert.True(t, st.State.Final())

	work.state = "failed"
	st, err = j.Get(ctx, ref.JobID)
	require.NoError(t, err)
	assert.Equal(t, JobFailed, st.State)
	assert.Equal(t, "no take", st.Error)
}

func TestJobs_TheQueueRowDecidesQueuedRunningAndFailureReason(t *testing.T) {
	ctx := context.Background()
	table := fakeTable{"q1": {ID: "q1", Kind: domain.JobGenerate, Status: domain.JobQueued}}
	j, _ := newJobs(t, table)
	work := &shotWork{state: "done", takes: []string{"t"}}
	j.RegisterKind("generate_shot", work.probe)
	ref, err := j.Run(ctx, "generate_shot", "shot-1", func(context.Context) (string, error) { return "q1", nil })
	require.NoError(t, err)
	assert.Equal(t, "q1", ref.JobID, "the queue's own id is used when the service gives it")

	for status, want := range map[domain.JobStatus]JobState{domain.JobQueued: JobQueued, domain.JobRunning: JobRunning} {
		table["q1"] = domain.Job{ID: "q1", Status: status}
		st, err := j.Get(ctx, "q1")
		require.NoError(t, err)
		assert.Equal(t, want, st.State, "a %s row beats a probe that sees old Takes", status)
	}

	done := time.Date(2026, 10, 24, 12, 5, 0, 0, time.UTC)
	table["q1"] = domain.Job{ID: "q1", Status: domain.JobFailed, Error: "interrupted by shutdown", FinishedAt: &done}
	st, err := j.Get(ctx, "q1")
	require.NoError(t, err)
	assert.Equal(t, JobFailed, st.State)
	assert.Equal(t, "interrupted by shutdown", st.Error, "the reason comes from the row")
	assert.Equal(t, &done, st.FinishedAt)

	table["q1"] = domain.Job{ID: "q1", Status: domain.JobSucceeded}
	st, err = j.Get(ctx, "q1")
	require.NoError(t, err)
	assert.Equal(t, JobSucceeded, st.State)
	assert.Equal(t, []string{"t"}, st.Result["take_ids"], "the probe still supplies the result")

	work.state = "running" // the job succeeded but a later stage (judge) of the same work runs
	st, err = j.Get(ctx, "q1")
	require.NoError(t, err)
	assert.Equal(t, JobRunning, st.State, "a succeeded row does not end work the probe sees running")
}

func TestJobs_AQueueRowIdWorksWithoutTheRegistry(t *testing.T) {
	ctx := context.Background()
	started := time.Date(2026, 10, 24, 11, 0, 0, 0, time.UTC)
	j, _ := newJobs(t, fakeTable{"q9": {ID: "q9", Kind: domain.JobPlan, Status: domain.JobRunning, CreatedAt: started}})

	st, err := j.Get(ctx, "q9")
	require.NoError(t, err)
	assert.Equal(t, JobRunning, st.State)
	assert.Equal(t, "plan", st.Kind)

	_, err = j.Get(ctx, "job_unknown")
	assert.ErrorIs(t, err, service.ErrNotFound)
	nj, _ := newJobs(t, nil)
	_, err = nj.Get(ctx, "q9")
	assert.ErrorIs(t, err, service.ErrNotFound, "without a job table only registered ids are known")
}

func TestJobs_RunRefusesWhatCannotBeFollowed(t *testing.T) {
	ctx := context.Background()
	j, _ := newJobs(t, nil)

	boom := errors.New("busy")
	_, err := j.Run(ctx, "k", "x", func(context.Context) (string, error) { return "", boom })
	assert.ErrorIs(t, err, boom, "the service's error comes back as it is")
	open, err := j.history.Open(ctx, 10)
	require.NoError(t, err)
	assert.Empty(t, open, "a start that failed leaves no job")

	_, err = j.Run(ctx, "unprobed", "x", func(context.Context) (string, error) { return "", nil })
	assert.Error(t, err, "no probe and no queue id: there would be nothing to read the state from")
	open, err = j.history.Open(ctx, 10)
	require.NoError(t, err)
	assert.Empty(t, open)
	assert.Panics(t, func() { j.RegisterKind("k", nil); j.RegisterKind("k", nil) })
}

func TestJobs_RunResolvedTakesTheTargetFromTheStartedWork(t *testing.T) {
	ctx := context.Background()
	j, _ := newJobs(t, nil)
	work := &shotWork{state: "running"}
	j.RegisterKind("generate_shot", work.probe)

	ref, err := j.RunResolved(ctx, "generate_shot", func(context.Context) (string, string, error) { return "new-shot", "", nil })
	require.NoError(t, err)
	st, err := j.Get(ctx, ref.JobID)
	require.NoError(t, err)
	assert.Equal(t, "new-shot", st.Target, "the target the work made, not one known before it started")

	_, err = j.RunResolved(ctx, "generate_shot", func(context.Context) (string, string, error) { return "", "", errors.New("refused") })
	assert.EqualError(t, err, "refused", "a start that fails makes no job")
}
