package generation_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
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
