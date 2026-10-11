package generation_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

func rejected() error {
	return &modeladapter.ProviderError{Vendor: "fake", Status: 400, Message: "content policy"}
}

func TestOrchestrator_GenerateKeepsThePaidCostWhenTheVideoCannotBeStored(t *testing.T) {
	f := newFixture(t, options{})
	// A regular file where the assets directory should be: the provider charges, the local store fails.
	require.NoError(t, os.WriteFile(f.assetsDir, []byte("x"), 0o600))

	err := f.generate(t, "")

	require.Error(t, err)
	assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM take`), "no Take was kept")
	paid := f.video.callCount()
	require.Positive(t, paid)
	assert.Equal(t, paid, f.count(t, `SELECT COUNT(*) FROM cost_ledger_entry WHERE shot_id = ? AND take_id IS NULL AND estimated_cost > 0`, f.shotID),
		"every video the provider made is on the ledger")
	b, err := f.gens.Budget(context.Background(), f.projectID, f.shotID)
	require.NoError(t, err)
	assert.Positive(t, b.ShotSpent)
	assert.Positive(t, b.ProjectSpent)
}

func TestOrchestrator_GenerateMakesEveryTakeAndRecordsThem(t *testing.T) {
	f := newFixture(t, options{withJudge: true})

	require.NoError(t, f.generate(t, ""))

	// Balanced policy: 3 Takes with the primary model, all stored with their file, cost and card.
	assert.Equal(t, 3, f.count(t, `SELECT COUNT(*) FROM take WHERE model_name = 'seedance-2.0' AND status = 'candidate'`))
	assert.Equal(t, 3, f.count(t, `SELECT COUNT(*) FROM cost_ledger_entry WHERE provider = 'seedance-2.0' AND shot_id = ? AND estimated_cost > 0`, f.shotID))
	assert.Equal(t, 3, f.storedFiles(t), "every Take has its video file")
	assert.Len(t, f.chat.ofKind(domain.KindTake), 3)
	assert.Len(t, f.chat.ofKind(domain.KindProgress), 1)
	assert.Len(t, f.judge.startedIDs(), 3, "every Take is handed to the AI Video Judge as soon as it exists")

	// The Canonical Shot Spec is stored on the Shot and as a prompt version snapshot.
	spec, ok := f.ref(t).Shot.CanonicalSpec()
	require.True(t, ok)
	assert.Equal(t, "Character_A", spec.Character.ID)
	assert.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM prompt_version WHERE shot_id = ?`, f.shotID))
	assert.Equal(t, 1, f.llm.calls(llm.AgentPromptPlanner))
	assert.Equal(t, 1, f.llm.calls(llm.AgentStateExtractor), "the World State is updated once per Shot")

	// The Shot is given back and the progress ends with the counts.
	assert.Equal(t, domain.ShotPlanning, f.shotState(t))
	assert.Equal(t, []generation.Stage{generation.StageQueued, generation.StageGenerating}, f.progress.stages()[:2])
	last := f.progress.last()
	assert.Equal(t, generation.StageDone, last.Stage)
	assert.Equal(t, 3, last.TakesDone)
	assert.Equal(t, 3, last.TakesTotal)
}

func TestOrchestrator_SecondGenerationKeepsTheSpecUnlessThereIsAnInstruction(t *testing.T) {
	f := newFixture(t, options{})
	require.NoError(t, f.generate(t, ""))
	require.NoError(t, f.generate(t, ""))
	assert.Equal(t, 1, f.llm.calls(llm.AgentPromptPlanner), "a retake reuses the Canonical Shot Spec")
	assert.Equal(t, 6, f.count(t, `SELECT COUNT(*) FROM take`))
	assert.Equal(t, 2, f.count(t, `SELECT COUNT(*) FROM prompt_version`), "every generation is a prompt version")

	require.NoError(t, f.generate(t, "もう少しカメラを寄せて"))
	assert.Equal(t, 2, f.llm.calls(llm.AgentPromptPlanner), "an instruction makes the Prompt Planner revise the spec")
	assert.Contains(t, f.llm.lastUser(llm.AgentPromptPlanner), "もう少しカメラを寄せて")
	assert.Equal(t, 9, f.count(t, `SELECT COUNT(*) FROM take`))
}

func TestOrchestrator_PartialTakeFailureKeepsTheTakesThatSucceeded(t *testing.T) {
	// Only the primary model has a backend, so a failed call has no alternative to fall back to.
	f := newFixture(t, options{videoModels: []domain.ModelName{domain.ModelSeedance2}})
	f.video.failOn = func(n int) error {
		if n == 2 {
			return rejected()
		}
		return nil
	}

	require.NoError(t, f.generate(t, ""), "one failed Take does not fail the job")

	assert.Equal(t, 2, f.count(t, `SELECT COUNT(*) FROM take`), "the Takes that succeeded are persisted")
	assert.Equal(t, 2, f.count(t, `SELECT COUNT(*) FROM cost_ledger_entry WHERE shot_id = ?`, f.shotID), "a failed call is not charged")
	assert.Equal(t, 2, f.storedFiles(t))
	assert.Len(t, f.chat.ofKind(domain.KindTake), 2)
	last := f.progress.last()
	assert.Equal(t, generation.StageDone, last.Stage)
	assert.Equal(t, 2, last.TakesDone)
	assert.Equal(t, 3, last.TakesTotal)
	assert.Equal(t, domain.ShotPlanning, f.shotState(t))
	assert.Empty(t, f.chat.ofKind(domain.KindText), "no failure notice: the director has Takes to look at")
}

func TestOrchestrator_AllTakesFailingFailsTheJobAndTellsTheUser(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantReason string
	}{
		{"request rejected", rejected(), "拒否"},
		{"api key missing", fmt.Errorf("seedance: %w", modeladapter.ErrNoAPIKey), "API キー"},
		{"rate limited", &modeladapter.ProviderError{Vendor: "fake", Status: 429, Message: "slow down"}, "レート制限"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t, options{})
			f.video.failOn = func(int) error { return c.err }

			err := f.generate(t, "")

			require.Error(t, err)
			assert.Contains(t, err.Error(), c.wantReason)
			assert.NotContains(t, err.Error(), "fake", "the vendor's details stay in the log")
			assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM take`))
			assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM cost_ledger_entry`))
			assert.Zero(t, f.storedFiles(t))
			assert.Equal(t, domain.ShotPlanning, f.shotState(t), "the Shot is released so the user can try again")
			notices := f.chat.ofKind(domain.KindText)
			require.Len(t, notices, 1)
			assert.Contains(t, notices[0].Content, "失敗しました")
			assert.Contains(t, notices[0].Content, c.wantReason)
			last := f.progress.last()
			assert.Equal(t, generation.StageFailed, last.Stage)
			assert.Contains(t, last.Message, c.wantReason)
		})
	}
}

func TestOrchestrator_AlternativeModelMakesTheTakesWhenThePrimaryHasNoBackend(t *testing.T) {
	// Only the alternative (Kling) has a backend: the primary cannot be called.
	f := newFixture(t, options{videoModels: []domain.ModelName{domain.ModelKling3}})

	require.NoError(t, f.generate(t, ""))

	assert.Equal(t, 3, f.count(t, `SELECT COUNT(*) FROM take WHERE model_name = 'kling-3.0'`))
	var params string
	require.NoError(t, f.db.Reader().QueryRow(`SELECT model_parameters FROM take LIMIT 1`).Scan(&params))
	assert.Contains(t, params, `"fell_back":true`)
}

func TestOrchestrator_PlannerFailureFailsTheJobBeforeAnyVideoCall(t *testing.T) {
	f := newFixture(t, options{})
	f.llm.errs = map[llm.Agent]error{llm.AgentPromptPlanner: llm.ErrNoAPIKey}

	err := f.generate(t, "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "API キー")
	assert.Zero(t, f.video.callCount(), "no paid call is made without a spec")
	assert.Equal(t, domain.ShotPlanning, f.shotState(t))
}

func TestOrchestrator_CancellationReleasesTheShotWithoutABotherNotice(t *testing.T) {
	f := newFixture(t, options{})
	f.video.block = make(chan struct{})
	f.video.entered = make(chan struct{}, 8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	job, err := f.orch.Start(ctx, f.shotID, "")
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() { done <- f.orch.Handle(ctx, job) }()
	<-f.video.entered // a video call is in flight
	cancel()

	require.Error(t, <-done)
	assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM take`))
	assert.Zero(t, f.storedFiles(t))
	assert.Equal(t, domain.ShotPlanning, f.shotState(t), "the Shot is released although the context is gone")
	assert.Empty(t, f.chat.ofKind(domain.KindText), "nobody is told about a shutdown")
	for _, p := range f.progress.all() {
		assert.NotEqual(t, generation.StageFailed, p.Stage)
	}
}

func TestOrchestrator_GenerationTimeLimitFailsTheTake(t *testing.T) {
	f := newFixture(t, options{limits: &domain.PolicyLimits{MaxGenerationSeconds: new(domain.MinGenerationSeconds)}})
	f.video.block = make(chan struct{}) // the model never answers

	err := f.generate(t, "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "制限時間")
	assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM take`))
}

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
			assert.Contains(t, warnings[0].Content, f.projectID)
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

func TestOrchestrator_HandleRejectsAnInvalidPayload(t *testing.T) {
	f := newFixture(t, options{})
	for name, payload := range map[string]string{
		"not json":       `nope`,
		"no shot":        `{"action":"generate"}`,
		"unknown action": `{"action":"dance","shot_id":"x"}`,
	} {
		t.Run(name, func(t *testing.T) {
			err := f.orch.Handle(context.Background(), domain.Job{ID: "j", Payload: []byte(payload)})
			assert.ErrorContains(t, err, "invalid payload")
		})
	}

	err := f.orch.Handle(context.Background(), domain.Job{ID: "j", Payload: []byte(`{"action":"generate","shot_id":"missing"}`)})
	assert.ErrorIs(t, err, repository.ErrNotFound)
}

func TestFailureReason(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"no model key", fmt.Errorf("x: %w", modeladapter.ErrNoAPIKey), "API キー"},
		{"no llm key", llm.ErrNoAPIKey, "API キー"},
		{"budget", fmt.Errorf("x: %w", generation.ErrBudgetExceeded), "予算上限"},
		{"brand", domain.ErrBrandViolation, "Brand"},
		{"judge off", generation.ErrJudgeUnavailable, "AI Video Judge"},
		{"time limit", fmt.Errorf("%w (1s)", generation.ErrGenerationTimeLimit), "制限時間"},
		{"rate limit", &modeladapter.ProviderError{Status: 429}, "レート制限"},
		{"circuit open", fmt.Errorf("x: %w", modeladapter.ErrCircuitOpen), "一時的に利用できません"},
		{"llm circuit open", fmt.Errorf("x: %w", llm.ErrCircuitOpen), "AI サービスが一時的に利用できません"},
		{"rejected", modeladapter.ErrRejected, "拒否"},
		{"model timeout", modeladapter.ErrTimeout, "タイムアウト"},
		{"llm timeout", llm.ErrTimeout, "タイムアウト"},
		{"ffmpeg timeout", fmt.Errorf("%w after 30s", media.ErrTimeout), "ffmpeg"},
		{"bad llm output", llm.ErrInvalidOutput, "AI の出力"},
		{"provider error", &modeladapter.ProviderError{Status: 502}, "生成モデルの呼び出しに失敗"},
		{"llm api error", &llm.APIError{Status: 500}, "AI サービスの呼び出しに失敗"},
		{"unknown", errors.New("sql: database is locked"), "内部エラー"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := generation.FailureReason(c.err)
			assert.Contains(t, got, c.want)
			assert.NotContains(t, got, "sql:", "internal details are not shown to the user")
		})
	}
}

type failingVideo struct{ err error }

func (f failingVideo) GenerateVideo(context.Context, string, modeladapter.VideoRequest) (modeladapter.VideoResult, error) {
	return modeladapter.VideoResult{}, f.err
}

// A primary model skipped for its open breaker must not make the user wait and retry when the alternative
// refused the request: the reason of the alternative is what is reported (#439).
func TestFailureReason_NamesTheAlternativesRejectionWhenThePrimaryIsSkippedByItsBreaker(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	cfg.Retry.MaxAttempts = 1
	cfg.Breaker.FailureThreshold = 1
	a := modeladapter.New(cfg, modeladapter.Backends{Video: map[domain.ModelName]modeladapter.VideoBackend{
		domain.ModelSeedance2: failingVideo{&modeladapter.ProviderError{Vendor: string(modeladapter.VendorSeedance), Status: 502}},
		domain.ModelKling3:    failingVideo{rejected()},
	}}, modeladapter.WithSleep(func(context.Context, time.Duration) error { return nil }))
	seedance := modeladapter.VideoTarget{Model: domain.ModelSeedance2, Resolution: "720p"}
	kling := modeladapter.VideoTarget{Model: domain.ModelKling3, Resolution: "720p"}
	job := modeladapter.VideoJob{Spec: domain.ShotSpec{ShotObjective: "run", Camera: domain.SpecCamera{Framing: "medium"}}, DurationSeconds: 4}
	_, err := a.GenerateVideo(context.Background(), seedance, seedance, job) // opens the Seedance breaker
	require.Error(t, err)

	_, err = a.GenerateVideo(context.Background(), seedance, kling, job)

	require.Error(t, err)
	assert.Contains(t, generation.FailureReason(err), "拒否")
	assert.NotContains(t, generation.FailureReason(err), "一時的に利用できません")
}

func TestFixedModel(t *testing.T) {
	assert.Empty(t, generation.FixedModel(domain.ParameterLocks{}), "AUTO leaves the model to the routing")
}

// The Shot is claimed before the job is queued: a job that ends before its work starts (here the
// Shot cannot be read) must still give the Shot back, or every new generation is refused as busy.
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
