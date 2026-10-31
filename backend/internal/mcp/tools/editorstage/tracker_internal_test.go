package editorstage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/logging"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// These tests change the package's early failure window and so are not parallel: the parallel tests of
// this package start only after the serial ones ended.

func newTracker4Test(t *testing.T) (*tracker, *mcptest.Env) {
	t.Helper()
	old := earlyFailureWindow
	earlyFailureWindow = 30 * time.Millisecond
	t.Cleanup(func() { earlyFailureWindow = old })
	env := mcptest.New(t, func(*mcptest.Env, *mcpkit.Registrar) {})
	tr := newTracker(env.Registrar)
	env.Registrar.Jobs.RegisterKind("slow_stage", tr.probe("working"), mcpkit.InMemory())
	return tr, env
}

func status(t *testing.T, env *mcptest.Env, id string) mcpkit.JobStatus {
	t.Helper()
	st, err := env.Registrar.Jobs.Get(context.Background(), id)
	require.NoError(t, err)
	return st
}

func waitFinal(t *testing.T, env *mcptest.Env, id string) mcpkit.JobStatus {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		st := status(t, env, id)
		if st.State.Final() {
			return st
		}
		require.True(t, time.Now().Before(deadline), "job did not end: %+v", st)
		time.Sleep(5 * time.Millisecond)
	}
}

func TestTracker_ASlowStageIsRunningThenSucceededWithItsResult(t *testing.T) {
	tr, env := newTracker4Test(t)
	release := make(chan struct{})
	ref, err := tr.start(context.Background(), "slow_stage", "p1", func(context.Context) (map[string]any, error) {
		<-release
		return map[string]any{"export_id": "e1"}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, mcpkit.JobQueued, ref.State)

	st := status(t, env, ref.JobID)
	assert.Equal(t, mcpkit.JobRunning, st.State)
	assert.Equal(t, "working", st.Message)
	assert.Equal(t, "p1", st.Target)

	close(release)
	st = waitFinal(t, env, ref.JobID)
	assert.Equal(t, mcpkit.JobSucceeded, st.State)
	assert.Equal(t, "e1", st.Result["export_id"])
	assert.Equal(t, mcpkit.JobSucceeded, status(t, env, ref.JobID).State, "the same answer once it ended")
}

func TestTracker_ASlowStageThatFailsReportsTheScreensError(t *testing.T) {
	tr, env := newTracker4Test(t)
	release := make(chan struct{})
	ref, err := tr.start(context.Background(), "slow_stage", "p1", func(context.Context) (map[string]any, error) {
		<-release
		return nil, &service.ConflictError{Message: "音声が最新の編集に対して作られていません"}
	})
	require.NoError(t, err)
	close(release)

	st := waitFinal(t, env, ref.JobID)
	assert.Equal(t, mcpkit.JobFailed, st.State)
	assert.Equal(t, "conflict: 音声が最新の編集に対して作られていません", st.Error)
}

func TestTracker_AStageThatFailsAtOnceIsTheErrorOfTheCallAndMakesNoJob(t *testing.T) {
	tr, _ := newTracker4Test(t)
	ref, err := tr.start(context.Background(), "slow_stage", "p1", func(context.Context) (map[string]any, error) {
		return nil, service.ErrStageBusy
	})
	require.ErrorIs(t, err, service.ErrStageBusy)
	assert.Empty(t, ref.JobID)
	assert.Empty(t, tr.runs)
}

func TestTracker_APanickingStageFailsTheJobAndLeavesTheProcessAlive(t *testing.T) {
	tr, env := newTracker4Test(t)
	release := make(chan struct{})
	ref, err := tr.start(context.Background(), "slow_stage", "p1", func(context.Context) (map[string]any, error) {
		<-release
		panic("boom")
	})
	require.NoError(t, err)
	close(release)

	st := waitFinal(t, env, ref.JobID)
	assert.Equal(t, mcpkit.JobFailed, st.State)
	assert.NotContains(t, st.Error, "boom", "the cause stays in the log, not in the message")
}

func TestTracker_ForgetsTheOldestRunWhenFull(t *testing.T) {
	tr, _ := newTracker4Test(t)
	for i := range maxRuns + 2 {
		tr.remember(fmt.Sprint("job_", i), &outcome{done: make(chan struct{})})
	}
	assert.Len(t, tr.runs, maxRuns)
	assert.Len(t, tr.order, maxRuns)
}

// An internal failure of a stage has a correlation id: in the log line of its cause, in the job's error and
// in the history row, so that list_jobs and get_job lead to the cause without it being in the message.
func TestTracker_AnInternalFailureCarriesTheIdOfItsLogLine(t *testing.T) {
	tr, env := newTracker4Test(t)
	release := make(chan struct{})
	ref, err := tr.start(context.Background(), "slow_stage", "p1", func(context.Context) (map[string]any, error) {
		<-release
		return nil, errors.New("open /secret/path: boom")
	})
	require.NoError(t, err)
	close(release)

	st := waitFinal(t, env, ref.JobID)
	assert.Equal(t, mcpkit.JobFailed, st.State)
	assert.Equal(t, "internal", st.ErrorCode)
	require.NotEmpty(t, st.ErrorID)
	assert.Contains(t, st.Error, "error_id: "+st.ErrorID)
	assert.NotContains(t, st.Error, "/secret/path")

	res, err := logging.Reader{Dir: env.LogDir}.Read(context.Background(), logging.Query{
		Limit: 5, MinLevel: slog.LevelDebug, Attrs: map[string]string{mcpkit.ErrorIDKey: st.ErrorID}})
	require.NoError(t, err)
	require.Len(t, res.Entries, 1)
	assert.Equal(t, "mcp editing stage failed", res.Entries[0].Message)
	assert.Equal(t, "slow_stage", res.Entries[0].Attrs["kind"])
	assert.Contains(t, res.Entries[0].Attrs["err"], "boom")

	// The history row has it, and the next process reads it from there.
	after := env.Restart(func(_ *mcptest.Env, r *mcpkit.Registrar) {
		r.Jobs.RegisterKind("slow_stage", newTracker(r).probe("working"), mcpkit.InMemory()) // a new tracker: it knows nothing
	})
	got, err := after.Registrar.Jobs.Get(context.Background(), ref.JobID)
	require.NoError(t, err)
	assert.Equal(t, st.ErrorID, got.ErrorID)
	assert.Equal(t, st.Error, got.Error)
}

func TestTracker_AStageThatFailsAtOnceWithAnInternalErrorReturnsTheIdToo(t *testing.T) {
	tr, env := newTracker4Test(t)
	_, err := tr.start(context.Background(), "slow_stage", "p1", func(context.Context) (map[string]any, error) {
		return nil, errors.New("disk exploded")
	})
	var te *mcpkit.ToolError
	require.ErrorAs(t, err, &te)
	require.NotEmpty(t, te.ErrorID)
	res, err := logging.Reader{Dir: env.LogDir}.Read(context.Background(), logging.Query{
		Limit: 5, MinLevel: slog.LevelDebug, Attrs: map[string]string{mcpkit.ErrorIDKey: te.ErrorID}})
	require.NoError(t, err)
	assert.Len(t, res.Entries, 1, "one line for one failure")
}
