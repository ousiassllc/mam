package generation_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

func TestOrchestrator_StartRefusesWhenNoTakeFitsTheBudget(t *testing.T) {
	cases := []struct {
		name    string
		project *float64
		shot    *float64
	}{
		{"project ceiling", new(0.0001), nil},
		{"shot ceiling", nil, new(0.0001)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t, options{})
			require.NoError(t, f.policies.SetBudget(context.Background(), f.projectID, c.project, c.shot))

			_, err := f.orch.Start(context.Background(), f.shotID, "")

			assert.ErrorIs(t, err, generation.ErrBudgetExceeded)
			assert.Zero(t, f.jobs.count(), "nothing is queued")
			assert.Equal(t, domain.ShotPlanning, f.shotState(t), "the Shot is not claimed")
			assert.Zero(t, f.video.callCount())
		})
	}
}

func TestOrchestrator_BudgetShrinksTheNumberOfTakes(t *testing.T) {
	f := newFixture(t, options{})
	// Seedance 2.0 at 720p for 4 s costs a few tenths of a dollar per Take; allow roughly one.
	cfg := modeladapter.DefaultConfig()
	perTake := cfg.VideoCost(cfg.Policies[domain.QualityBalanced].Primary, 4)
	require.NoError(t, f.policies.SetBudget(context.Background(), f.projectID, new(perTake*1.5), nil))

	require.NoError(t, f.generate(t, ""))

	assert.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM take`), "only one Take fits the project ceiling")
	assert.Equal(t, 1, f.progress.last().TakesDone)
}

func TestOrchestrator_BudgetSpentWhileQueuedFailsTheJob(t *testing.T) {
	f := newFixture(t, options{})
	job, err := f.orch.Start(context.Background(), f.shotID, "")
	require.NoError(t, err)
	require.NoError(t, f.policies.SetBudget(context.Background(), f.projectID, new(0.0001), nil))

	err = f.orch.Handle(context.Background(), job)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "予算上限")
	assert.Zero(t, f.video.callCount())
	assert.Equal(t, generation.StageFailed, f.progress.last().Stage)
}

func TestOrchestrator_StartWarnsAboutLowDiskButStillQueues(t *testing.T) {
	cases := []struct {
		name     string
		free     func(string) (uint64, error)
		wantWarn bool
	}{
		{"not enough space", func(string) (uint64, error) { return 1024, nil }, true},
		{"plenty of space", func(string) (uint64, error) { return 1 << 40, nil }, false},
		{"space unknown", func(string) (uint64, error) { return 0, errors.New("statfs failed") }, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t, options{freeBytes: c.free})

			job, err := f.orch.Start(context.Background(), f.shotID, "")

			require.NoError(t, err, "a low disk warns, it does not stop the generation")
			assert.Equal(t, 1, f.jobs.count())
			assert.NotEmpty(t, job.ID)
			warnings := f.chat.ofKind(domain.KindText)
			if !c.wantWarn {
				assert.Empty(t, warnings)
				return
			}
			require.Len(t, warnings, 1)
			assert.Contains(t, warnings[0].Content, "ディスクの空き容量")
			assert.Equal(t, domain.RefSettings, warnings[0].RefType, "the chat draws the settings link; the text has no URL")
		})
	}
}

func TestOrchestrator_StartRefusesASecondRunAndReleasesOnQueueFailure(t *testing.T) {
	f := newFixture(t, options{})
	_, err := f.orch.Start(context.Background(), f.shotID, "")
	require.NoError(t, err)

	_, err = f.orch.Start(context.Background(), f.shotID, "")
	assert.ErrorIs(t, err, generation.ErrShotBusy)
	_, err = f.orch.StartReferencePack(context.Background(), f.shotID)
	assert.ErrorIs(t, err, generation.ErrShotBusy)

	g := newFixture(t, options{})
	g.jobs.err = errors.New("queue is closed")
	_, err = g.orch.Start(context.Background(), g.shotID, "")
	require.Error(t, err)
	assert.Equal(t, domain.ShotPlanning, g.shotState(t), "a Shot whose job could not be queued is not stuck GENERATING")

	_, err = f.orch.Start(context.Background(), "missing", "")
	assert.ErrorIs(t, err, repository.ErrNotFound)
}

func TestFixedModel(t *testing.T) {
	assert.Empty(t, generation.FixedModel(domain.ParameterLocks{}), "AUTO leaves the model to the routing")
}

// The Shot is claimed before the job is queued: a job ending before its work starts must still give the Shot back (else it is busy).
func TestHandle_ReleasesTheShotWhenTheJobEndsBeforeItsWorkStarts(t *testing.T) {
	f := newFixture(t, options{})
	job, err := f.orch.Start(context.Background(), f.shotID, "")
	require.NoError(t, err)
	require.Equal(t, domain.ShotGenerating, f.shotState(t))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.Error(t, f.orch.Handle(ctx, job))

	assert.Equal(t, domain.ShotPlanning, f.shotState(t))
	_, err = f.orch.Start(context.Background(), f.shotID, "")
	assert.NoError(t, err, "the Shot accepts the next generation")
}

// A Shot that is already being generated or repaired is refused before the Prompt Planner (a paid
// LLM call) writes a spec that would be thrown away.
func TestOrchestrator_StartOfABusyShotSparesThePromptPlanner(t *testing.T) {
	bg := context.Background()
	for _, state := range []domain.ShotState{domain.ShotGenerating, domain.ShotRepairing} {
		for name, start := range map[string]func(f *fixture) error{
			"Start":           func(f *fixture) error { _, err := f.orch.Start(bg, f.shotID, ""); return err },
			"StartDirected":   func(f *fixture) error { _, err := f.orch.StartDirected(bg, f.shotID, "slower", nil); return err },
			"StartCandidates": func(f *fixture) error { _, err := f.orch.StartCandidates(bg, f.shotID, ""); return err },
		} {
			t.Run(string(state)+"/"+name, func(t *testing.T) {
				f := newFixture(t, options{withJudge: true})
				require.NoError(t, f.gens.SetShotState(context.Background(), f.shotID, state))

				err := start(f)

				assert.ErrorIs(t, err, generation.ErrShotBusy)
				assert.Zero(t, f.llm.calls(llm.AgentPromptPlanner), "the Planner is not paid for a Shot that is busy")
				assert.Zero(t, f.jobs.count())
			})
		}
	}
}

func TestOrchestrator_TakeInFlightCountsAgainstTheBudgetUntilItIsRecorded(t *testing.T) {
	f := newFixture(t, options{})
	cfg := modeladapter.DefaultConfig()
	perTake := cfg.VideoCost(cfg.Policies[domain.QualityBalanced].Primary, 4)
	// One Take fits, a second does not: the first is not on the ledger while the vendor is working.
	require.NoError(t, f.policies.SetBudget(context.Background(), f.projectID, new(perTake*1.5), nil))
	f.video.block = make(chan struct{})
	f.video.entered = make(chan struct{}, 8)
	job, err := f.orch.Start(context.Background(), f.shotID, "")
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- f.orch.Handle(context.Background(), job) }()
	<-f.video.entered // the Take is being made

	b, err := f.gens.Budget(context.Background(), f.projectID, f.shotID)
	require.NoError(t, err)
	assert.InDelta(t, perTake, b.ProjectSpent, 1e-9, "the estimate of the Take in flight is held")
	other, cancel := context.WithTimeout(context.Background(), 2*time.Second) // bounds it where the budget is not held
	defer cancel()
	_, err = f.orch.Regenerate(other, f.ref(t), generation.Regeneration{Action: domain.RepairFullRetake})
	assert.ErrorIs(t, err, generation.ErrBudgetExceeded, "another generation does not see the whole budget as free")

	close(f.video.block)
	require.NoError(t, <-done)
	b, err = f.gens.Budget(context.Background(), f.projectID, f.shotID)
	require.NoError(t, err)
	assert.InDelta(t, perTake, b.ProjectSpent, 1e-9, "counted once, from the ledger, after the Take is recorded")
}
