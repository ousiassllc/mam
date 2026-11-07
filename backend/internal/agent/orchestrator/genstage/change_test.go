package genstage_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/genapprovalrepo"
)

// cardText is the body of the newest generation card.
func (r *rig) cardText(projectID string) string {
	r.t.Helper()
	var text string
	require.NoError(r.t, r.db.Reader().QueryRow(`SELECT content FROM chat_message WHERE id = ?`, r.card(projectID).ID).Scan(&text))
	return text
}

// A change of the scope after the approval (a Shot added or deleted, another video model, another Quality Policy) is not covered by it: a new card
// is posted, nothing is started until that one is approved, and the new card says what changed.
func TestGenerationApproval_AChangedScopeAfterTheApprovalAsksAgainAndStartsNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	changes := map[string]struct {
		change func(r *rig, id string, shots []string)
		reason string
	}{
		"a Shot is added": {func(r *rig, id string, _ []string) {
			scenes, err := r.plans.ListScenes(ctx, id)
			require.NoError(t, err)
			_, err = r.plans.CreateShot(ctx, domain.Shot{SceneID: scenes[0].ID, Objective: "追加", Duration: 4})
			require.NoError(t, err)
		}, "追加された Shot（4）"},
		"a Shot is deleted": {func(r *rig, _ string, shots []string) {
			require.NoError(t, r.plans.RemoveShots(ctx, shots[1:2], nil))
		}, "削除された Shot（2）"},
		"the video model of a Shot is fixed to another": {func(r *rig, _ string, shots []string) {
			require.NoError(t, repository.NewLockRepository(r.db).SetShotLock(ctx, shots[0], domain.ParamVideoModel, domain.LockLocked, json.RawMessage(`"`+string(domain.VideoModels()[0])+`"`)))
		}, "モデルや本数が変わった Shot（1）"},
		"the Quality Policy changes the models and the Takes": {func(r *rig, id string, _ []string) {
			require.NoError(t, r.policies.SetQualityPolicy(ctx, id, domain.QualityFast, nil))
		}, "モデルや本数が変わった Shot（1・2・3）"},
	}
	for name, c := range changes {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := newRig(t)
			id := r.approved(domain.ConfirmFullAuto)
			c.change(r, id, r.shotIDs(id))

			r.handle(id)
			r.handle(id)

			assert.Equal(t, 2, r.cards(id), "one new card, however often it is looked at")
			assert.Equal(t, domain.ApprovalPending, r.card(id).Status)
			assert.Equal(t, domain.StateAwaitingApproval, r.project1(id).State)
			assert.Empty(t, r.gen.started(), "nothing is started until the new scope is approved")
			assert.Contains(t, r.cardText(id), "承認し直しが必要です")
			assert.Contains(t, r.cardText(id), c.reason)

			r.answer(id, domain.ApprovalApproved)
			r.handle(id)
			assert.NotEmpty(t, r.gen.started(), "the new approval carries the plan on")
		})
	}
}

// A change after the Reference Packs were started holds the Shots back too: the generation of a Shot whose pack is made waits for the new approval.
func TestGenerationApproval_AChangeInTheMiddleHoldsEvenTheShotsWhosePackIsMade(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := newRig(t)
	id := r.approved(domain.ConfirmFullAuto)
	shots := r.shotIDs(id)
	r.handle(id)
	require.Equal(t, 3, len(r.gen.started()))
	scenes, err := r.plans.ListScenes(ctx, id)
	require.NoError(t, err)
	added, err := r.plans.CreateShot(ctx, domain.Shot{SceneID: scenes[0].ID, Objective: "追加", Duration: 4})
	require.NoError(t, err)
	r.endJob(shots[0], false)

	r.handle(id)

	assert.Equal(t, 3, len(r.gen.started()), "Shot 1's pack is made, and its generation waits for the new approval")
	assert.Equal(t, 2, r.cards(id))
	r.answer(id, domain.ApprovalApproved)
	r.handle(id)
	assert.Equal(t, []call{{genapprovalrepo.StageGenerate, shots[0]}, {genapprovalrepo.StageReferencePack, added.ID}}, r.gen.started()[3:],
		"approved again: the Shot with a pack is generated and the new Shot gets its pack; the packs that run are not started twice")
}

// The price: a rise after the approval asks again, a fall does not. The estimate is compared for the part that is still to be made.
func TestGenerationApproval_ARisingPriceAsksAgainAndAFallingOneDoesNot(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rise := func(r *rig, to float64) {
		r.prices.edit(func(c modeladapter.Config) { c.Models[domain.ModelSeedance2].CostPerSecond["720p"] = to })
	}
	t.Run("rises", func(t *testing.T) {
		t.Parallel()
		r := newRig(t)
		r.prices.pricesKnown()
		id := r.approved(domain.ConfirmFullAuto)
		require.Contains(t, r.cardText(id), "費用の見積: 最大")

		rise(r, 0.30)
		r.handle(id)

		assert.Equal(t, 2, r.cards(id))
		assert.Contains(t, r.cardText(id), "見積が承認時の")
		assert.Contains(t, r.cardText(id), "に増えました")
		assert.Empty(t, r.gen.started())
	})
	t.Run("falls", func(t *testing.T) {
		t.Parallel()
		r := newRig(t)
		r.prices.pricesKnown()
		id := r.approved(domain.ConfirmFullAuto)

		rise(r, 0.05)
		r.handle(id)

		assert.Equal(t, 1, r.cards(id), "a lower price needs no new approval")
		assert.Equal(t, 3, r.gen.count(genapprovalrepo.StageReferencePack))
	})
	t.Run("rises after the Reference Packs were started", func(t *testing.T) {
		t.Parallel()
		r := newRig(t)
		r.prices.pricesKnown()
		id := r.approved(domain.ConfirmFullAuto)
		r.handle(id)
		for _, s := range r.shotIDs(id) {
			r.endJob(s, false)
		}
		r.handle(id) // the generations are started at the approved price
		require.Equal(t, 6, len(r.gen.started()))
		_ = ctx
	})
}

// An estimate that cannot be made is "不明" and is never replaced by a number made up; the approval is still needed, and a change between a number and
// "不明" is a change that asks again.
func TestGenerationApproval_AnUnknownEstimateStillNeedsTheApprovalAndItsChangesAskAgain(t *testing.T) {
	t.Parallel()
	t.Run("unknown stays unknown", func(t *testing.T) {
		t.Parallel()
		r := newRig(t) // GPT Image 2.6 has no price in the default setting: the Reference Pack cannot be priced
		id := r.planned(domain.ConfirmFullAuto)
		r.handle(id)

		assert.Empty(t, r.gen.started(), "an estimate that is 不明 does not start anything by itself")
		assert.Contains(t, r.cardText(id), "不明")
		assert.NotContains(t, r.cardText(id), "費用の見積: 最大")
		r.answer(id, domain.ApprovalApproved)
		r.handle(id)
		assert.Equal(t, 1, r.cards(id), "不明 → 不明 is not a change")
		assert.Equal(t, 3, r.gen.count(genapprovalrepo.StageReferencePack))
	})
	t.Run("unknown becomes a number", func(t *testing.T) {
		t.Parallel()
		r := newRig(t)
		id := r.approved(domain.ConfirmFullAuto)
		r.prices.pricesKnown()

		r.handle(id)

		assert.Equal(t, 2, r.cards(id))
		assert.Contains(t, r.cardText(id), "「不明」から数字に変わりました")
		assert.Empty(t, r.gen.started())
	})
	t.Run("a number becomes unknown", func(t *testing.T) {
		t.Parallel()
		r := newRig(t)
		r.prices.pricesKnown()
		id := r.approved(domain.ConfirmFullAuto)
		r.prices.price(domain.ModelGPTImage26Flare, 0)

		r.handle(id)

		assert.Equal(t, 2, r.cards(id))
		assert.Contains(t, r.cardText(id), "数字から「不明」に変わりました")
		assert.Empty(t, r.gen.started())
	})
}

// The card says what will be made and what it costs, and the budget ceilings when they are set.
func TestGenerationApproval_TheCardSaysWhatIsMadeWithWhichModelsAndTheBudget(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.prices.pricesKnown()
	id := r.planned(domain.ConfirmFullAuto)
	require.NoError(t, r.policies.SetBudget(context.Background(), id, new(50.0), new(5.0)))

	r.handle(id)

	text := r.cardText(id)
	for _, want := range []string{
		"Shot 3 件（Shot 1・2・3）",
		"Reference Pack を作る Shot: 3 件", "gpt-image-2.6-flare / nano-banana", "最大 6 枚",
		"seedance-2.0（720p）", "1 回の生成", "Shot あたり最大 3 本", "合計最大 9 本",
		"費用の見積: 最大 ", "USD（Reference Pack 最大 ", "動画 最大 6.7500 USD", // 9 clips × 5 s × 0.15 USD
		"予算上限: Project 50.0000 USD", "Shot あたり 5.0000 USD",
		"含まれないもの", "承認すると自動で進めます",
	} {
		assert.Contains(t, text, want)
	}
}
