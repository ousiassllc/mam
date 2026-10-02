package generation_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

func (f *fixture) referencePack(t *testing.T) error {
	t.Helper()
	return f.run(context.Background(), t, func() (domain.Job, error) {
		return f.orch.StartReferencePack(context.Background(), f.shotID)
	})
}

func TestOrchestrator_ReferencePackMakesEveryReferenceAndFeedsTheNextGeneration(t *testing.T) {
	f := newFixture(t, options{})

	require.NoError(t, f.referencePack(t))

	refs := f.ref(t).Shot.References()
	kinds := map[domain.ReferenceKind]int{}
	for _, r := range refs {
		kinds[r.Kind]++
		assert.NotEmpty(t, r.AssetID)
		assert.NotEmpty(t, r.Model)
	}
	assert.Equal(t, map[domain.ReferenceKind]int{
		domain.RefCharacter: 1, domain.RefProduct: 1, domain.RefLocation: 1,
		domain.RefStyle: 1, domain.RefLighting: 1, domain.RefKeyframe: 1,
	}, kinds)
	assert.Equal(t, 6, f.image.callCount())
	assert.Equal(t, 6, f.storedFiles(t))
	assert.Equal(t, 6, f.count(t, `SELECT COUNT(*) FROM cost_ledger_entry WHERE shot_id = ? AND estimated_cost > 0`, f.shotID))
	assert.Len(t, f.chat.ofKind(domain.KindKeyframe), 6, "a card per image")
	assert.Equal(t, domain.ShotPlanning, f.shotState(t))
	last := f.progress.last()
	assert.Equal(t, generation.ActionReferencePack, last.Action)
	assert.Equal(t, generation.StageDone, last.Stage)
	assert.Equal(t, 6, last.TakesDone)

	// The Keyframe goes to the video model as the first frame; Seedance reads either frames or references,
	// so the other references are not sent with it.
	require.NoError(t, f.generate(t, ""))
	sent := f.video.sent()
	require.NotEmpty(t, sent)
	for _, req := range sent {
		assert.NotNil(t, req.FirstFrame, "the Keyframe is the first frame")
		assert.Empty(t, req.References, "frames and reference images are exclusive")
	}
}

func TestOrchestrator_ReferenceReinforcementSendsReferencesInsteadOfTheKeyframe(t *testing.T) {
	f := newFixture(t, options{})
	require.NoError(t, f.referencePack(t))
	require.NoError(t, f.generate(t, ""))
	before := len(f.video.sent())

	_, err := f.orch.Regenerate(context.Background(), f.ref(t), generation.Regeneration{
		Action: domain.RepairReferenceReinforce, Issues: []domain.JudgeIssue{{Type: domain.IssueTypeFaceIdentityDrift, Severity: domain.SeverityHigh}},
	})

	require.NoError(t, err)
	sent := f.video.sent()
	require.Greater(t, len(sent), before)
	req := sent[len(sent)-1]
	assert.Nil(t, req.FirstFrame, "the references are what is reinforced")
	assert.NotEmpty(t, req.References)
}

func TestOrchestrator_ReferencePackFailureKeepsNothing(t *testing.T) {
	cases := []struct {
		name       string
		failOn     func(n int) error
		wantReason string
		wantPaid   int // images the provider charged for before the failure
	}{
		// The five references are made first (calls 1-5), then the Keyframe (6), whose alternative model is call 7.
		{"the Keyframe is rejected after the references were made", func(n int) error {
			if n > 5 {
				return rejected()
			}
			return nil
		}, "拒否", 5},
		{"every image fails", func(int) error { return modeladapter.ErrNoAPIKey }, "API キー", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t, options{})
			f.image.failOn = c.failOn

			err := f.referencePack(t)

			require.Error(t, err)
			assert.Contains(t, err.Error(), c.wantReason)
			assert.Empty(t, f.ref(t).Shot.References(), "no half-built Pack is saved")
			assert.Zero(t, f.storedFiles(t), "images made before the failure are removed")
			assert.Equal(t, c.wantPaid, f.count(t, `SELECT COUNT(*) FROM cost_ledger_entry WHERE shot_id = ? AND estimated_cost > 0`, f.shotID),
				"images the provider charged for stay on the ledger even though the Pack is dropped")
			assert.Equal(t, c.wantPaid, f.count(t, `SELECT COUNT(*) FROM cost_ledger_entry`))
			b, berr := f.gens.Budget(context.Background(), f.projectID, f.shotID)
			require.NoError(t, berr)
			assert.Equal(t, c.wantPaid > 0, b.ShotSpent > 0 && b.ProjectSpent > 0, "the Budget counts the lost Pack")
			assert.Equal(t, domain.ShotPlanning, f.shotState(t))
			notices := f.chat.ofKind(domain.KindText)
			require.Len(t, notices, 1)
			assert.Contains(t, notices[0].Content, "Reference Pack の作成に失敗しました")
			assert.Equal(t, generation.StageFailed, f.progress.last().Stage)
		})
	}
}

func TestOrchestrator_StartReferencePackRefusesWhenNoImageFitsTheBudget(t *testing.T) {
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

			_, err := f.orch.StartReferencePack(context.Background(), f.shotID)

			assert.ErrorIs(t, err, generation.ErrBudgetExceeded)
			assert.Zero(t, f.jobs.count(), "nothing is queued")
			assert.Equal(t, domain.ShotPlanning, f.shotState(t), "the Shot is not claimed")
			assert.Zero(t, f.image.callCount())
		})
	}
}

func TestOrchestrator_ReferencePackArtDirectorFailure(t *testing.T) {
	f := newFixture(t, options{})
	f.llm.errs = map[llm.Agent]error{llm.AgentArtDirector: llm.ErrNoAPIKey}

	err := f.referencePack(t)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "API キー")
	assert.Zero(t, f.image.callCount())
}

func TestOrchestrator_ReferencePackCancelledMidwayReleasesTheShot(t *testing.T) {
	f := newFixture(t, options{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	job, err := f.orch.StartReferencePack(ctx, f.shotID)
	require.NoError(t, err)
	f.image.failOn = func(int) error {
		cancel() // the app shuts down while the first image is being made
		return context.Canceled
	}

	require.Error(t, f.orch.Handle(ctx, job))

	assert.Equal(t, domain.ShotPlanning, f.shotState(t))
	assert.Empty(t, f.chat.ofKind(domain.KindText), "nobody is told about a shutdown")
	assert.Empty(t, f.ref(t).Shot.References())
}

// generated returns a fixture whose Shot has a Canonical Shot Spec and three Takes.
func generated(t *testing.T, opt options) *fixture {
	t.Helper()
	f := newFixture(t, opt)
	require.NoError(t, f.generate(t, ""))
	return f
}

func TestOrchestrator_RegenerateMakesOneNewTakeWithoutTouchingTheOld(t *testing.T) {
	cases := []struct {
		name        string
		regen       generation.Regeneration
		wantPlanner int // Prompt Planner calls in total after the generation (1) and the repair
		wantModel   domain.ModelName
	}{
		{"full retake keeps the spec", generation.Regeneration{Action: domain.RepairFullRetake}, 1, domain.ModelSeedance2},
		{"prompt repair has the planner revise the spec", generation.Regeneration{
			Action: domain.RepairPrompt, Instructions: []string{"reduce_camera_speed_40_percent"},
			Issues: []domain.JudgeIssue{{Type: "camera_jitter", Severity: domain.SeverityMedium, TimeRange: "1.0-2.0"}},
		}, 2, domain.ModelSeedance2},
		{"partial regeneration preserves what was good", generation.Regeneration{
			Action: domain.RepairPartialRegeneration, Preserve: []string{"character", "background", "nonsense"},
			Ranges: []domain.TimeRange{{From: 1, To: 2}},
		}, 2, domain.ModelSeedance2},
		{"reference reinforcement", generation.Regeneration{
			Action: domain.RepairReferenceReinforce, Issues: []domain.JudgeIssue{{Type: domain.IssueTypeFaceIdentityDrift, Severity: domain.SeverityHigh}},
		}, 1, domain.ModelSeedance2},
		{"different model goes to the other model", generation.Regeneration{
			Action: domain.RepairDifferentModel, SourceModel: domain.ModelSeedance2,
		}, 1, domain.ModelKling3},
		{"direction of the director", generation.Regeneration{Action: domain.RepairPrompt, Direction: "make it warmer"}, 2, domain.ModelSeedance2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := generated(t, options{withJudge: true})
			before := f.count(t, `SELECT COUNT(*) FROM take`)
			old := f.count(t, `SELECT COUNT(*) FROM take WHERE status = 'candidate'`)

			id, err := f.orch.Regenerate(context.Background(), f.ref(t), c.regen)

			require.NoError(t, err)
			require.NotEmpty(t, id)
			assert.Equal(t, before+1, f.count(t, `SELECT COUNT(*) FROM take`))
			assert.Equal(t, old+1, f.count(t, `SELECT COUNT(*) FROM take WHERE status = 'candidate'`), "the repaired Takes keep their status")
			assert.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM take WHERE id = ? AND model_name = ?`, id, string(c.wantModel)))
			assert.Equal(t, c.wantPlanner, f.llm.calls(llm.AgentPromptPlanner))
			assert.Contains(t, f.judge.startedIDs(), id, "the new Take is judged")
			assert.Equal(t, before+1, f.count(t, `SELECT COUNT(*) FROM cost_ledger_entry WHERE shot_id = ?`, f.shotID), "the new Take is charged")
			assert.Equal(t, before+1, f.storedFiles(t))
		})
	}
}

func TestOrchestrator_RegenerateWarnsAboutLowDiskButStillMakesTheTake(t *testing.T) {
	cases := []struct {
		name     string
		free     func(string) (uint64, error)
		wantWarn bool
	}{
		{"not enough space", func(string) (uint64, error) { return 1024, nil }, true},
		{"plenty of space", func(string) (uint64, error) { return 1 << 40, nil }, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t, options{freeBytes: c.free})
			require.NoError(t, f.generate(t, ""))
			before := len(f.chat.ofKind(domain.KindText))

			id, err := f.orch.Regenerate(context.Background(), f.ref(t), generation.Regeneration{Action: domain.RepairFullRetake})

			require.NoError(t, err, "a low disk warns, it does not stop the repair")
			require.NotEmpty(t, id)
			warnings := f.chat.ofKind(domain.KindText)
			if !c.wantWarn {
				assert.Len(t, warnings, before)
				return
			}
			require.Len(t, warnings, before+1)
			assert.Contains(t, warnings[before].Content, "ディスクの空き容量")
		})
	}
}

func TestOrchestrator_RegeneratePromptGetsTheIssuesAndInstructions(t *testing.T) {
	f := generated(t, options{})

	_, err := f.orch.Regenerate(context.Background(), f.ref(t), generation.Regeneration{
		Action: domain.RepairPartialRegeneration, Direction: "slower push in",
		Issues:       []domain.JudgeIssue{{Type: "camera_jitter", Severity: domain.SeverityMedium, TimeRange: "1.0-2.0", Description: "shaky"}},
		Instructions: []string{"reduce_camera_speed_40_percent"}, Preserve: []string{"character"},
		Ranges: []domain.TimeRange{{From: 1, To: 2}},
	})

	require.NoError(t, err)
	prompt := f.llm.lastUser(llm.AgentPromptPlanner)
	for _, want := range []string{"camera_jitter", "1.0-2.0", "shaky", "slower push in", "reduce_camera_speed_40_percent", "1.0-2.0s", "character"} {
		assert.Contains(t, prompt, want)
	}
}

func TestOrchestrator_RegenerateRefusals(t *testing.T) {
	t.Run("an action that does not regenerate", func(t *testing.T) {
		f := generated(t, options{})
		_, err := f.orch.Regenerate(context.Background(), f.ref(t), generation.Regeneration{Action: domain.RepairEditLevel})
		require.Error(t, err)
	})
	t.Run("a Shot without a spec", func(t *testing.T) {
		f := newFixture(t, options{})
		_, err := f.orch.Regenerate(context.Background(), f.ref(t), generation.Regeneration{Action: domain.RepairFullRetake})
		assert.ErrorIs(t, err, generation.ErrNoSpec)
	})
	t.Run("a locked prompt cannot be revised", func(t *testing.T) {
		f := generated(t, options{})
		require.NoError(t, repository.NewLockRepository(f.db).SetShotLock(context.Background(), f.shotID, domain.ParamPrompt, domain.LockLocked, nil))
		before := f.video.callCount()
		_, err := f.orch.Regenerate(context.Background(), f.ref(t), generation.Regeneration{Action: domain.RepairPrompt})
		var locked *repository.LockedParametersError
		require.ErrorAs(t, err, &locked)
		assert.Equal(t, before, f.video.callCount(), "nothing is paid for")
	})
	t.Run("a fixed video model leaves no other model", func(t *testing.T) {
		f := generated(t, options{})
		require.NoError(t, repository.NewLockRepository(f.db).SetShotLock(context.Background(), f.shotID, domain.ParamVideoModel, domain.LockLocked, []byte(`"kling-3.0"`)))
		_, err := f.orch.Regenerate(context.Background(), f.ref(t), generation.Regeneration{Action: domain.RepairDifferentModel, SourceModel: domain.ModelKling3})
		assert.ErrorIs(t, err, generation.ErrNoOtherModel)
	})
	t.Run("the budget is spent", func(t *testing.T) {
		f := generated(t, options{})
		require.NoError(t, f.policies.SetBudget(context.Background(), f.projectID, new(0.0001), nil))
		before := f.video.callCount()
		_, err := f.orch.Regenerate(context.Background(), f.ref(t), generation.Regeneration{Action: domain.RepairFullRetake})
		assert.ErrorIs(t, err, generation.ErrBudgetExceeded)
		assert.Equal(t, before, f.video.callCount())
	})
}

func TestOrchestrator_RegenerateModelFailureLeavesTheTakesAlone(t *testing.T) {
	f := generated(t, options{videoModels: []domain.ModelName{domain.ModelSeedance2}})
	before := f.count(t, `SELECT COUNT(*) FROM take`)
	f.video.failOn = func(int) error { return rejected() }

	_, err := f.orch.Regenerate(context.Background(), f.ref(t), generation.Regeneration{Action: domain.RepairFullRetake})

	require.ErrorIs(t, err, modeladapter.ErrRejected)
	assert.Equal(t, before, f.count(t, `SELECT COUNT(*) FROM take`))
	assert.Equal(t, before, f.storedFiles(t))
}

func TestOrchestrator_RegenerationStrategy(t *testing.T) {
	f := generated(t, options{})

	s, err := f.orch.RegenerationStrategy(context.Background(), f.ref(t), generation.Regeneration{Action: domain.RepairFullRetake})
	require.NoError(t, err)
	assert.Equal(t, 1, s.Takes, "a repair makes one Take")
	assert.Equal(t, domain.ModelSeedance2, s.Primary.Model)
	assert.Positive(t, s.CostPerTake)

	s, err = f.orch.RegenerationStrategy(context.Background(), f.ref(t), generation.Regeneration{Action: domain.RepairDifferentModel, SourceModel: domain.ModelSeedance2})
	require.NoError(t, err)
	assert.Equal(t, domain.ModelKling3, s.Primary.Model)
	assert.Equal(t, domain.ModelSeedance2, s.Fallback.Model)
	assert.Contains(t, s.Reasons[len(s.Reasons)-1], "different model")

	// A source model that is neither of the Quality Policy's models has the policy's primary as "other".
	s, err = f.orch.RegenerationStrategy(context.Background(), f.ref(t), generation.Regeneration{Action: domain.RepairDifferentModel, SourceModel: domain.ModelKling3})
	require.NoError(t, err)
	assert.Equal(t, domain.ModelSeedance2, s.Primary.Model)
}

func TestOrchestrator_GenerateHonoursAFixedVideoModel(t *testing.T) {
	f := newFixture(t, options{})
	require.NoError(t, repository.NewLockRepository(f.db).SetShotLock(context.Background(), f.shotID, domain.ParamVideoModel, domain.LockLocked, []byte(`"kling-3.0"`)))

	require.NoError(t, f.generate(t, ""))

	assert.Equal(t, 3, f.count(t, `SELECT COUNT(*) FROM take WHERE model_name = 'kling-3.0'`))
	assert.Equal(t, domain.ModelKling3, generation.FixedModel(mustLocks(t, f)))
}

func mustLocks(t *testing.T, f *fixture) domain.ParameterLocks {
	t.Helper()
	l, err := f.gens.ShotLocks(context.Background(), f.shotID)
	require.NoError(t, err)
	return l
}

func TestOrchestrator_ReferenceReinforcementFailOverModelKeepsTheKeyframe(t *testing.T) {
	f := newFixture(t, options{})
	require.NoError(t, f.referencePack(t))
	require.NoError(t, f.generate(t, ""))
	before := f.video.callCount()
	// The primary (Seedance, free references) times out; the alternative (Kling, frames only) takes over.
	f.video.failOn = func(n int) error {
		if n == before+1 {
			return modeladapter.ErrTimeout
		}
		return nil
	}

	_, err := f.orch.Regenerate(context.Background(), f.ref(t), generation.Regeneration{
		Action: domain.RepairReferenceReinforce, Issues: []domain.JudgeIssue{{Type: domain.IssueTypeFaceIdentityDrift, Severity: domain.SeverityHigh}},
	})

	require.NoError(t, err)
	sent := f.video.sent()
	require.Len(t, sent, before+2)
	assert.Nil(t, sent[before].FirstFrame)
	assert.NotEmpty(t, sent[before].References, "the primary reads the reinforced references")
	assert.NotNil(t, sent[before+1].FirstFrame, "the alternative cannot use references: it keeps the Keyframe")
	assert.Empty(t, sent[before+1].References)
}
