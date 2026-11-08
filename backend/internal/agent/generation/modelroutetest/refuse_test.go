package modelroutetest_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/modelroute"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/modelroute/routetest"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// assertRefused checks the refusal of a start: the typed error with its Japanese reason, and that nothing was queued, claimed, paid or announced.
func assertRefused(t *testing.T, r *rig, err error) *modelroute.NoUsableError {
	t.Helper()
	require.ErrorIs(t, err, modelroute.ErrNoUsableModel)
	var nu *modelroute.NoUsableError
	require.ErrorAs(t, err, &nu)
	assert.Zero(t, r.jobs.count(), "no job is queued")
	assert.Equal(t, domain.ShotPlanning, r.shot(t).Shot.State, "the Shot is not claimed")
	assert.Empty(t, r.chat.all(), "no progress card is posted")
	images, videos := r.log.snapshot()
	assert.Empty(t, images)
	assert.Empty(t, videos)
	assert.Zero(t, r.count(t, `SELECT COUNT(*) FROM cost_ledger_entry`))
	return nu
}

func TestStart_NothingUsableIsRefusedBeforeAJobIsQueuedWithTheCauseAndTheWaysOut(t *testing.T) {
	for name, start := range map[string]func(*rig) error{
		"reference pack": func(r *rig) error { _, err := r.orch.StartReferencePack(context.Background(), r.shotID); return err },
		"generate":       func(r *rig) error { _, err := r.orch.Start(context.Background(), r.shotID, ""); return err },
		"by policy": func(r *rig) error {
			_, err := r.orch.StartByPolicy(context.Background(), r.shotID, "", nil)
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, nil)
			r.usable = nothingUsable

			err := start(r)

			nu := assertRefused(t, r, err)
			assert.False(t, nu.Explicit)
			assert.NotEmpty(t, nu.Assigned, "the models of the assignment, with why each cannot be used")
			reason := generation.FailureReason(err)
			assert.Contains(t, reason, "使えるモデルがありません")
			assert.Contains(t, reason, "API キーが登録されていません", "the cause of each model of the assignment")
			assert.Contains(t, reason, "API キーを登録する", "the ways out")
			assert.Contains(t, reason, "割り当て")
			assert.Equal(t, "no_usable_model", string(generation.Failure(err).Code))
		})
	}
}

func TestStart_ACustomisedAssignmentThatCannotBeUsedIsNotReplacedAndTheUsableModelsAreSuggested(t *testing.T) {
	r := newRig(t, func(cfg *modeladapter.Config) {
		cfg.Images = map[modeladapter.ImagePurpose]modeladapter.ImageRoute{}
		for k, v := range modeladapter.DefaultConfig().Images {
			cfg.Images[k] = v
		}
		cfg.Images[modeladapter.PurposeNewReference] = modeladapter.ImageRoute{
			Primary: modeladapter.ImageTarget{Model: domain.ModelFlux2}, Fallback: modeladapter.ImageTarget{Model: domain.ModelNanoBananaLite},
		}
		cfg.Policies = map[domain.QualityPolicy]modeladapter.PolicyRoute{}
		for k, v := range modeladapter.DefaultConfig().Policies {
			cfg.Policies[k] = v
		}
		cfg.Policies[domain.QualityBalanced] = modeladapter.PolicyRoute{
			Primary: modeladapter.VideoTarget{Model: domain.ModelKling3, Resolution: "720p"}, Fallback: modeladapter.VideoTarget{Model: domain.ModelRunwayGen45, Resolution: "720p"}, Takes: 2,
		}
	})

	_, packErr := r.orch.StartReferencePack(context.Background(), r.shotID)
	nu := assertRefused(t, r, packErr)
	assert.True(t, nu.Explicit)
	assert.Contains(t, generation.FailureReason(packErr), "割り当てを設定で変えてあるため、別のモデルへは自動で替えません")
	assert.Contains(t, generation.FailureReason(packErr), string(routetest.LocalImage), "the usable models are suggested")

	_, startErr := r.orch.Start(context.Background(), r.shotID, "")
	nu = assertRefused(t, r, startErr)
	assert.True(t, nu.Explicit)
	assert.Contains(t, generation.FailureReason(startErr), string(routetest.LocalVideo2), "the Shot has no Keyframe yet: the text-to-video model is the one that matches")
}

func TestStart_APinnedAssignmentIsNeverReplacedEvenWhenItIsTheDefault(t *testing.T) {
	r := newRig(t, nil)
	r.adapter.Usable.SetPinned(true)

	_, err := r.orch.Start(context.Background(), r.shotID, "")

	nu := assertRefused(t, r, err)
	assert.True(t, nu.Pinned)
	assert.Contains(t, generation.FailureReason(err), "起動オプションで固定")
}

func TestStart_AFixedVideoModelThatCannotBeUsedIsNotReplaced(t *testing.T) {
	r := newRig(t, nil)
	require.NoError(t, repository.NewLockRepository(r.db).SetShotLock(context.Background(), r.shotID, domain.ParamVideoModel, domain.LockLocked, []byte(`"kling-3.0"`)))

	_, err := r.orch.Start(context.Background(), r.shotID, "")

	nu := assertRefused(t, r, err)
	assert.True(t, nu.Explicit)
	assert.Equal(t, domain.ModelKling3, nu.Assigned[0].Model)
}

func TestHandle_AJobWhoseModelsWentAwayWhileItWaitedFailsWithTheCauseInTheChat(t *testing.T) {
	r := newRig(t, nil)
	job, err := r.orch.StartReferencePack(context.Background(), r.shotID)
	require.NoError(t, err)
	r.usable = nothingUsable // the sign-in ended while the job waited in the queue

	err = r.orch.Handle(context.Background(), job)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "使えるモデルがありません")
	assert.Contains(t, r.chat.text(), "使えるモデルがありません", "the chat is told, with the ways out")
	assert.Contains(t, r.chat.text(), "API キーを登録する")
	images, _ := r.log.snapshot()
	assert.Empty(t, images, "nothing was generated")
	assert.Equal(t, domain.ShotPlanning, r.shot(t).Shot.State, "the Shot is given back")
}
