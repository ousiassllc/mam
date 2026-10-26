package generation_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/agegate"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// closeGateFor makes the age gate refuse the models (the adult models while the age confirmation is off).
func (f *fixture) closeGateFor(models ...domain.ModelName) {
	f.adapter.Gate.Set(func(m domain.ModelName) bool {
		for _, refused := range models {
			if m == refused {
				return true
			}
		}
		return false
	})
}

// An assignment that still names an adult model while the age confirmation is off is refused at the start, whichever slot
// names it or whether the user fixed it on the Shot: no job, no claim on the Shot, no call. The alternative never makes the
// Take instead, which would hand the user a Take of another model without the refusal.
func TestOrchestrator_StartRefusesARouteThatNamesARefusedModel(t *testing.T) {
	// Balanced: Seedance 2.0 is the primary, Kling 3.0 the alternative.
	for name, c := range map[string]struct {
		refused domain.ModelName
		locked  bool
	}{
		"primary":         {refused: domain.ModelSeedance2},
		"alternative":     {refused: domain.ModelKling3},
		"fixed on a Shot": {refused: domain.ModelRunwayGen45, locked: true},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, options{})
			if c.locked {
				require.NoError(t, repository.NewLockRepository(f.db).SetShotLock(context.Background(), f.shotID, domain.ParamVideoModel, domain.LockLocked, []byte(`"`+string(c.refused)+`"`)))
			}
			f.closeGateFor(c.refused)

			_, err := f.orch.Start(context.Background(), f.shotID, "")

			var refused *agegate.RefusedError
			require.ErrorAs(t, err, &refused)
			assert.Equal(t, c.refused, refused.Model)
			assert.ErrorIs(t, err, agegate.ErrRefused)
			assert.Zero(t, f.jobs.count(), "nothing is queued")
			assert.Equal(t, domain.ShotPlanning, f.shotState(t), "the Shot is not claimed")
			assert.Zero(t, f.video.callCount())
			assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM take`))
		})
	}
}

// A job that was queued before the age confirmation went off is refused when it runs, tells the user what to do, and calls no model.
func TestOrchestrator_AQueuedGenerationIsRefusedWhenTheGateClosesBeforeItRuns(t *testing.T) {
	f := newFixture(t, options{})
	job, err := f.orch.Start(context.Background(), f.shotID, "")
	require.NoError(t, err)
	f.closeGateFor(domain.ModelSeedance2)

	err = f.orch.Handle(context.Background(), job)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "seedance-2.0 は成人向けモデルで、年齢確認がオフのため使えません")
	assert.Zero(t, f.video.callCount(), "the model was not called")
	assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM take`))
	notices := f.chat.ofKind(domain.KindText)
	require.NotEmpty(t, notices)
	assert.Contains(t, notices[len(notices)-1].Content, "年齢確認をオンにするか")
	assert.Equal(t, domain.ShotPlanning, f.shotState(t), "the Shot is free again")
}

// The Reference Pack makes images of both purposes, so a route of either that names a refused model refuses it up front. The
// purposes name different models here, so each slot of each purpose is the only place its model is found.
func TestOrchestrator_StartReferencePackRefusesAnImageRouteThatNamesARefusedModel(t *testing.T) {
	to := func(m domain.ModelName) modeladapter.ImageTarget { return modeladapter.ImageTarget{Model: m} }
	for _, refused := range []domain.ModelName{domain.ModelGPTImage2, domain.ModelNanoBanana, domain.ModelFlux2, domain.ModelNanoBananaLite} {
		f := newFixture(t, options{})
		cfg, backends := f.adapter.Snapshot()
		cfg.Images[modeladapter.PurposeNewReference] = modeladapter.ImageRoute{Primary: to(domain.ModelGPTImage2), Fallback: to(domain.ModelNanoBanana)}
		cfg.Images[modeladapter.PurposeEditReference] = modeladapter.ImageRoute{Primary: to(domain.ModelFlux2), Fallback: to(domain.ModelNanoBananaLite)}
		f.adapter.Reload(cfg, backends)
		f.closeGateFor(refused)

		_, err := f.orch.StartReferencePack(context.Background(), f.shotID)

		require.ErrorIs(t, err, agegate.ErrRefused, refused)
		assert.Contains(t, err.Error(), string(refused))
		assert.Zero(t, f.jobs.count(), refused)
	}
}

// The age confirmation goes off while Prompt Candidates judge the first Take: the run stops before the next Take and the
// summary says why (the gate), not that the budget is spent.
func TestOrchestrator_ARetryRefusedByTheGateIsReportedAsSuchNotAsTheBudget(t *testing.T) {
	f := newFixture(t, options{withJudge: true})
	f.judge.verdicts = func(int) (domain.JudgeResult, error) {
		f.closeGateFor(domain.ModelSeedance2)
		return failing(), nil
	}

	require.NoError(t, f.candidates(t, ""))

	assert.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM take`), "the second Take was not made")
	summary := f.chat.ofKind(domain.KindText)
	require.Len(t, summary, 1)
	assert.Contains(t, summary[0].Content, "seedance-2.0 は成人向けモデルで、年齢確認がオフのため使えません")
	assert.NotContains(t, summary[0].Content, "予算上限")
}

// Opened again, the same assignment generates: the refusal is the gate's, not a change of the assignment.
func TestOrchestrator_TheSameAssignmentGeneratesOnceTheGateIsOpen(t *testing.T) {
	f := newFixture(t, options{})
	f.closeGateFor(domain.ModelSeedance2)
	_, err := f.orch.Start(context.Background(), f.shotID, "")
	require.ErrorIs(t, err, agegate.ErrRefused)

	f.adapter.Gate.Set(nil)

	require.NoError(t, f.generate(t, ""))
	assert.Equal(t, 3, f.count(t, `SELECT COUNT(*) FROM take WHERE model_name = 'seedance-2.0'`))
}
