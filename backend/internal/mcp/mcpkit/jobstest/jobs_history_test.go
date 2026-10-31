package jobstest_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/repository/jobhistory"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// process is one run of mam over a database that outlives it: a new process has new Jobs, so nothing the
// old one kept in memory is there.
type process struct {
	*mcpkit.Jobs
	clock *time.Time
}

func startProcess(t *testing.T, hist *jobhistory.Repository, clock *time.Time, table mcpkit.JobTable) process {
	t.Helper()
	now := func() time.Time { return *clock }
	j := mcpkit.NewJobs(table, hist.WithClock(now)).WithClock(now)
	return process{Jobs: j, clock: clock}
}

type memoryWork struct{ state, err string }

func (m *memoryWork) probe(_ context.Context, _ mcpkit.JobInfo) (mcpkit.Progress, error) {
	switch m.state {
	case "done":
		return mcpkit.Progress{State: mcpkit.JobSucceeded, Result: map[string]any{"verdict": "pass"}}, nil
	case "failed":
		return mcpkit.Progress{State: mcpkit.JobFailed, Error: m.err}, nil
	}
	return mcpkit.Progress{State: mcpkit.JobRunning, Message: "working"}, nil
}

func TestJobs_AnInMemoryJobIsAnsweredFromTheHistoryAfterARestart(t *testing.T) {
	ctx := context.Background()
	hist := jobhistory.New(testdb.Open(t))
	clock := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

	before := startProcess(t, hist, &clock, nil)
	work := &memoryWork{state: "run"}
	before.RegisterKind("judge_edit_timeline", work.probe, mcpkit.InMemory())
	ref, err := before.Run(ctx, "judge_edit_timeline", "tl-1", func(context.Context) (string, error) { return "", nil })
	require.NoError(t, err)
	clock = clock.Add(time.Minute)
	work.state = "done"
	want, err := before.Get(ctx, ref.JobID)
	require.NoError(t, err)
	require.Equal(t, mcpkit.JobSucceeded, want.State)
	require.NotNil(t, want.EndedAt)

	clock = clock.Add(time.Hour)
	after := startProcess(t, hist, &clock, nil)
	forgotten := &memoryWork{state: "failed", err: "nothing remembered"} // what a probe reads after a restart
	after.RegisterKind("judge_edit_timeline", forgotten.probe, mcpkit.InMemory())
	got, err := after.Get(ctx, ref.JobID)
	require.NoError(t, err)
	assert.Equal(t, want, got, "the same state, target, times and result as before the restart")
}

func TestJobs_AnInMemoryJobThatNobodySawEndIsFailedAfterARestart(t *testing.T) {
	ctx := context.Background()
	hist := jobhistory.New(testdb.Open(t))
	clock := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	before := startProcess(t, hist, &clock, nil)
	before.RegisterKind("generate_audio", (&memoryWork{state: "run"}).probe, mcpkit.InMemory())
	ref, err := before.Run(ctx, "generate_audio", "p-1", func(context.Context) (string, error) { return "", nil })
	require.NoError(t, err)

	clock = clock.Add(time.Hour)
	after := startProcess(t, hist, &clock, nil)
	after.RegisterKind("generate_audio", (&memoryWork{state: "run"}).probe, mcpkit.InMemory())
	got, err := after.Get(ctx, ref.JobID)
	require.NoError(t, err)
	assert.Equal(t, mcpkit.JobFailed, got.State, "a restart ended it")
	assert.Contains(t, got.Error, "再起動", "the restart is the reason")
	assert.Equal(t, "generate_audio", got.Kind)
	assert.Equal(t, "p-1", got.Target)
	require.NotNil(t, got.EndedAt)
	assert.Equal(t, clock.UTC(), got.EndedAt.UTC(), "the end is first known when mam looks after the restart")

	clock = clock.Add(time.Hour)
	again, err := after.Get(ctx, ref.JobID)
	require.NoError(t, err)
	assert.Equal(t, got, again, "the end is recorded once")
}

func TestJobs_AJobThatReadsPersistedStateAnswersAsBeforeAfterARestart(t *testing.T) {
	ctx := context.Background()
	hist := jobhistory.New(testdb.Open(t))
	clock := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	table := fakeTable{}
	work := &shotWork{state: "running"}

	before := startProcess(t, hist, &clock, table)
	before.RegisterKind("generate_shot", work.probe)
	ref, err := before.Run(ctx, "generate_shot", "shot-1", func(context.Context) (string, error) {
		table["q1"] = domain.Job{ID: "q1", Kind: domain.JobGenerate, Status: domain.JobQueued}
		return "q1", nil
	})
	require.NoError(t, err)
	assert.Equal(t, "q1", ref.JobID)

	clock = clock.Add(time.Hour)
	after := startProcess(t, hist, &clock, table)
	after.RegisterKind("generate_shot", work.probe)
	st, err := after.Get(ctx, "q1")
	require.NoError(t, err)
	assert.Equal(t, mcpkit.JobQueued, st.State, "the row decides, as it did before the restart")
	assert.Equal(t, "shot-1", st.Target, "the target comes from the history, not from the row")
	assert.Equal(t, time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC), st.StartedAt.UTC())

	finished := clock
	table["q1"] = domain.Job{ID: "q1", Status: domain.JobFailed, Error: "interrupted by shutdown", FinishedAt: &finished}
	st, err = after.Get(ctx, "q1")
	require.NoError(t, err)
	assert.Equal(t, mcpkit.JobFailed, st.State)
	assert.Equal(t, "interrupted by shutdown", st.Error)
	require.NotNil(t, st.EndedAt)
	assert.True(t, finished.Equal(*st.EndedAt), "the job queue's finish time is the end")

	delete(table, "q1") // the row is gone: the recorded end remains
	st, err = after.Get(ctx, "q1")
	require.NoError(t, err)
	assert.Equal(t, mcpkit.JobFailed, st.State)
	assert.Equal(t, "interrupted by shutdown", st.Error)

	_, err = after.Get(ctx, "job_unknown")
	assert.ErrorIs(t, err, service.ErrNotFound)
}

func TestJobs_ARowThatVanishedBeforeTheEndWasSeenIsNotFound(t *testing.T) {
	ctx := context.Background()
	clock := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	table := fakeTable{}
	j := startProcess(t, jobhistory.New(testdb.Open(t)), &clock, table)
	j.RegisterKind("generate_shot", (&shotWork{state: "running"}).probe)
	_, err := j.Run(ctx, "generate_shot", "s", func(context.Context) (string, error) { table["q2"] = domain.Job{ID: "q2"}; return "q2", nil })
	require.NoError(t, err)
	delete(table, "q2")
	_, err = j.Get(ctx, "q2")
	assert.ErrorIs(t, err, service.ErrNotFound, "as before: a job whose row is missing and whose end is unknown")
}

func TestJobs_TheProjectOfTheTargetIsRecordedAndErrorCodesAreRead(t *testing.T) {
	ctx := context.Background()
	clock := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	db := testdb.Open(t)
	j := startProcess(t, jobhistory.New(db), &clock, nil)
	work := &memoryWork{state: "failed", err: "budget_exceeded: 予算上限を超えるため生成できません"}
	j.RegisterKind("k", work.probe)
	ref, err := j.Run(ctx, "k", "no-such-target", func(context.Context) (string, error) { return "", nil })
	require.NoError(t, err)
	st, err := j.Get(ctx, ref.JobID)
	require.NoError(t, err)
	assert.Equal(t, mcpkit.JobFailed, st.State)
	assert.Equal(t, "budget_exceeded", st.ErrorCode)
	assert.Empty(t, st.ProjectID)

	work.err = "interrupted by shutdown" // a reason of the job queue has no code
	st, err = j.Get(ctx, ref.JobID)
	require.NoError(t, err)
	assert.Empty(t, st.ErrorCode)
}

func TestJobs_ObserveAndRefreshOpenRecordTheEnd(t *testing.T) {
	ctx := context.Background()
	hist := jobhistory.New(testdb.Open(t))
	clock := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	j := startProcess(t, hist, &clock, nil)
	work := &memoryWork{state: "run"}
	j.RegisterKind("k", work.probe)
	ref, err := j.Run(ctx, "k", "t", func(context.Context) (string, error) { return "", nil })
	require.NoError(t, err)

	j.Observe(ctx, ref.JobID)
	h, err := hist.Get(ctx, ref.JobID)
	require.NoError(t, err)
	assert.Equal(t, "running", h.State)
	assert.Equal(t, "working", h.Message)

	work.state = "done"
	require.NoError(t, j.RefreshOpen(ctx))
	h, err = hist.Get(ctx, ref.JobID)
	require.NoError(t, err)
	assert.Equal(t, "succeeded", h.State)
	assert.NotNil(t, h.EndedAt)
	assert.Empty(t, h.Result, "the result is kept only for kinds whose work cannot be read again")
	j.Observe(ctx, "job_unknown") // not an error
}

// A reader that probed before the end and records after another reader recorded the end (a client polling
// while Observe runs at the end of a stage) must neither move the end later nor take the job back to running.
func TestJobs_ConcurrentReadersKeepTheFirstEndOfAnInMemoryJob(t *testing.T) {
	for name, slowReaderSees := range map[string]string{"also ended": "done", "still running": "run"} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			hist := jobhistory.New(testdb.Open(t))
			clock := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
			j := startProcess(t, hist, &clock, nil)
			entered, release := make(chan struct{}), make(chan struct{})
			work := &memoryWork{}
			var calls atomic.Int32
			j.RegisterKind("judge_edit_timeline", func(ctx context.Context, info mcpkit.JobInfo) (mcpkit.Progress, error) {
				if calls.Add(1) == 1 { // the slow reader: it stops inside the probe until the other one is done
					close(entered)
					<-release
					return (&memoryWork{state: slowReaderSees}).probe(ctx, info)
				}
				return work.probe(ctx, info)
			}, mcpkit.InMemory())
			ref, err := j.Run(ctx, "judge_edit_timeline", "tl-1", func(context.Context) (string, error) { return "", nil })
			require.NoError(t, err)

			slow := make(chan mcpkit.JobStatus, 1)
			go func() {
				st, _ := j.Get(ctx, ref.JobID)
				slow <- st
			}()
			<-entered
			work.state = "done"
			clock = clock.Add(time.Minute)
			first, err := j.Get(ctx, ref.JobID)
			require.NoError(t, err)
			require.Equal(t, mcpkit.JobSucceeded, first.State)
			require.NotNil(t, first.EndedAt)

			clock = clock.Add(time.Minute)
			close(release)
			got := <-slow
			assert.Equal(t, mcpkit.JobSucceeded, got.State)
			require.NotNil(t, got.EndedAt)
			assert.True(t, first.EndedAt.Equal(*got.EndedAt), "the end is the time the first reader saw it")
			h, err := hist.Get(ctx, ref.JobID)
			require.NoError(t, err)
			assert.Equal(t, "succeeded", h.State)
			assert.True(t, first.EndedAt.Equal(*h.EndedAt))
			assert.JSONEq(t, `{"verdict":"pass"}`, string(h.Result))
		})
	}
}
