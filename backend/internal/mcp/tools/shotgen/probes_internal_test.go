package shotgen

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// The run's own writes (the result, the Take linked to its record) happen before the Shot is released.
// The fakes make the run end between the Probe's two kinds of reads: the trace read first is a step
// behind, the Shot read finds the Shot released, and the trace read after that has the run's result.

// fakeTraces serves the trace as the run has written it so far; finish writes the run's result.
type fakeTraces struct {
	trace domain.TakeTrace
	calls int
}

func (f *fakeTraces) Take(context.Context, string) (domain.TakeTrace, error) {
	f.calls++
	return f.trace, nil
}

// fakeShot is a Shot whose run ends while the Probe reads it: the read first lets afterRead happen (the
// run writes its result and releases the Shot), then tells the state that read finds.
type fakeShot struct {
	state     domain.ShotState
	afterRead func()
}

func (f *fakeShot) Shot(context.Context, string) (service.ShotFragment, error) {
	if f.afterRead != nil {
		f.afterRead()
	}
	frag := service.ShotFragment{}
	frag.State = f.state
	return frag, nil
}

var (
	started = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	job     = mcpkit.JobInfo{ID: "job", Target: "take", StartedAt: started}
)

func traceOf(mod func(*domain.TakeTrace)) domain.TakeTrace {
	tr := domain.TakeTrace{Take: domain.Take{ID: "take", ShotID: "shot"}}
	if mod != nil {
		mod(&tr)
	}
	return tr
}

func TestJudgeProbe_AResultWrittenBeforeTheShotWasReleasedIsSeenAfterTheShotReadShowsItReleased(t *testing.T) {
	stale := traceOf(nil)
	done := traceOf(func(tr *domain.TakeTrace) {
		tr.Judgements = []domain.JudgeResult{{Result: domain.JudgeFail, OverallConfidence: 0.5, CreatedAt: started.Add(time.Second)}}
	})
	traces := &fakeTraces{trace: stale}
	shot := &fakeShot{state: domain.ShotPlanning, afterRead: func() { traces.trace = done }}

	p, err := judgeProbe(shot, traces)(context.Background(), job)
	require.NoError(t, err)
	assert.Equal(t, mcpkit.JobSucceeded, p.State, p.Error)
	assert.Equal(t, "fail", p.Result["verdict"])
	assert.Equal(t, 2, traces.calls, "the trace is read again once the Shot is known to be released")
}

func TestJudgeProbe_AReleasedShotWithoutAResultFailsAndAJudgingShotRuns(t *testing.T) {
	traces := &fakeTraces{trace: traceOf(nil)}
	p, err := judgeProbe(&fakeShot{state: domain.ShotPlanning}, traces)(context.Background(), job)
	require.NoError(t, err)
	assert.Equal(t, mcpkit.JobFailed, p.State)

	traces = &fakeTraces{trace: traceOf(nil)}
	p, err = judgeProbe(&fakeShot{state: domain.ShotJudging}, traces)(context.Background(), job)
	require.NoError(t, err)
	assert.Equal(t, mcpkit.JobRunning, p.State)
	assert.Equal(t, 1, traces.calls, "a running Shot needs no second read")
}

func TestJudgeProbe_AResultOlderThanTheJobIsNotTheRunsResult(t *testing.T) {
	old := traceOf(func(tr *domain.TakeTrace) {
		tr.Judgements = []domain.JudgeResult{{Result: domain.JudgePass, CreatedAt: started.Add(-time.Minute)}}
	})
	p, err := judgeProbe(&fakeShot{state: domain.ShotPlanning}, &fakeTraces{trace: old})(context.Background(), job)
	require.NoError(t, err)
	assert.Equal(t, mcpkit.JobFailed, p.State)
}

func TestRepairProbe_ATakeLinkedBeforeTheShotWasReleasedIsSeenAfterTheShotReadShowsItReleased(t *testing.T) {
	unlinked := traceOf(func(tr *domain.TakeTrace) {
		tr.Repairs = []domain.RepairRecord{{Action: domain.RepairFullRetake, CreatedAt: started.Add(time.Second)}}
	})
	linked := traceOf(func(tr *domain.TakeTrace) {
		tr.Repairs = []domain.RepairRecord{{Action: domain.RepairFullRetake, ResultingTakeID: "new-take", CreatedAt: started.Add(time.Second)}}
	})
	traces := &fakeTraces{trace: unlinked}
	shot := &fakeShot{state: domain.ShotPlanning, afterRead: func() { traces.trace = linked }}

	p, err := repairProbe(shot, traces)(context.Background(), job)
	require.NoError(t, err)
	assert.Equal(t, mcpkit.JobSucceeded, p.State, p.Error)
	assert.Equal(t, "new-take", p.Result["new_take_id"])
	assert.Equal(t, 2, traces.calls, "the trace is read again once the Shot is known to be released")
}

func TestRepairProbe_FailuresAndEditLevelRepairAreToldAsBefore(t *testing.T) {
	record := func(action domain.RepairAction, newTake string) domain.TakeTrace {
		return traceOf(func(tr *domain.TakeTrace) {
			tr.Repairs = []domain.RepairRecord{{Action: action, ResultingTakeID: newTake, CreatedAt: started.Add(time.Second)}}
		})
	}
	for name, tc := range map[string]struct {
		tr    domain.TakeTrace
		state domain.ShotState
		want  mcpkit.JobState
	}{
		"repairing shot":            {record(domain.RepairFullRetake, ""), domain.ShotRepairing, mcpkit.JobRunning},
		"regeneration without take": {record(domain.RepairFullRetake, ""), domain.ShotPlanning, mcpkit.JobFailed},
		"no record at all":          {traceOf(nil), domain.ShotPlanning, mcpkit.JobFailed},
		"edit level makes no take":  {record(domain.RepairEditLevel, ""), domain.ShotPlanning, mcpkit.JobSucceeded},
	} {
		t.Run(name, func(t *testing.T) {
			p, err := repairProbe(&fakeShot{state: tc.state}, &fakeTraces{trace: tc.tr})(context.Background(), job)
			require.NoError(t, err)
			assert.Equal(t, tc.want, p.State, p.Error)
		})
	}
}
