package modelroutetest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/chatgptid"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/modelroute/routetest"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/modelreg"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
)

// The observed configuration of Issue #1113 (v0.1.34): no API key of any vendor is registered, so the default assignment (GPT Image 2.6 Flare, Nano Banana,
// Seedance 2.0, Kling 3.0) points at nothing that can be used, while the ChatGPT subscription's image model, ComfyUI's image models and a local image-to-video
// workflow can. The Reference Pack and the video run on the models that match, and the Takes say which and why.
func TestObservedConfiguration_TheDefaultAssignmentRunsOnTheUsableModels(t *testing.T) {
	r := newRig(t, nil)

	require.NoError(t, r.runPack(t))
	require.NoError(t, r.runGenerate(t))

	images, videos := r.log.snapshot()
	require.NotEmpty(t, images)
	for _, m := range images {
		assert.Equal(t, routetest.LocalImage, m, "every image of the pack is made on this PC: the models of the assignment have no key")
	}
	require.Len(t, videos, 3, "the Balanced policy's three Takes")
	for _, m := range videos {
		assert.Equal(t, routetest.LocalVideo, m, "the image-to-video workflow that holds the Shot and takes its Keyframe")
	}

	refs := r.shot(t).Shot.References()
	require.NotEmpty(t, refs)
	for _, ref := range refs {
		require.NotNil(t, ref.Use)
		assert.Equal(t, string(routetest.LocalImage), ref.Use.Model)
		assert.Equal(t, modelreg.RouteLocal, ref.Use.Route)
		assert.Equal(t, modelreg.SelectionCondition, ref.Use.Selection)
		assert.Equal(t, modelreg.CostFree, ref.Use.CostBasis)
	}
	uses := r.takeUses(t)
	require.Len(t, uses, 3)
	for _, u := range uses {
		assert.Equal(t, string(routetest.LocalVideo), u.Model)
		assert.Equal(t, modelreg.RouteLocal, u.Route)
		assert.Equal(t, modelreg.SelectionCondition, u.Selection)
		assert.Contains(t, u.Reason, "条件に合う利用可能なモデルを選びました")
		assert.NotEmpty(t, u.Passed, "the models of the assignment that were passed over, and why")
		assert.Contains(t, u.Passed[0].Why, "API キー")
		assert.Equal(t, modelreg.CostFree, u.CostBasis)
	}
	assert.Zero(t, r.count(t, `SELECT COUNT(*) FROM cost_ledger_entry WHERE estimated_cost > 0`), "a model on this PC is free: 0 USD")
	assert.Contains(t, r.chat.text(), "ローカル・無料・条件に合うモデルを自動選択", "the cards of the chat say the route")
	assert.Contains(t, r.chat.text(), "使う画像モデル: new_reference＝"+string(routetest.LocalImage), "the progress card of the pack names the models it was started with")
	assert.Contains(t, r.chat.text(), "使う動画モデル: "+string(routetest.LocalVideo), "and so does the one of the generation")
}

func TestObservedConfiguration_TheSubscriptionIsNextWhenThisPCHasNoImageModel(t *testing.T) {
	r := newRig(t, nil)
	r.usable = only(chatgptid.Flare, chatgptid.Sunburst, chatgptid.Model)

	require.NoError(t, r.runPack(t))

	images, _ := r.log.snapshot()
	require.NotEmpty(t, images)
	for _, m := range images {
		assert.Equal(t, chatgptid.Flare, m)
	}
	for _, ref := range r.shot(t).Shot.References() {
		require.NotNil(t, ref.Use)
		assert.Equal(t, modelreg.RouteSubscription, ref.Use.Route)
		assert.Equal(t, modelreg.CostUnknown, ref.Use.CostBasis, "the plan pays: not free, and not counted")
	}
}

func TestObservedConfiguration_AUsableAssignmentIsNotReplaced(t *testing.T) {
	r := newRig(t, nil)
	r.usable = func(domain.ModelName) modelfilter.Availability { return modelfilter.Usable() } // the keys are registered

	require.NoError(t, r.runPack(t))
	require.NoError(t, r.runGenerate(t))

	images, videos := r.log.snapshot()
	for _, m := range images {
		assert.Contains(t, []domain.ModelName{domain.ModelGPTImage26Flare, domain.ModelNanoBanana}, m, "the assignment's own models, as before")
	}
	for _, m := range videos {
		assert.Equal(t, domain.ModelSeedance2, m)
	}
	for _, u := range r.takeUses(t) {
		assert.Equal(t, modelreg.SelectionAssignment, u.Selection)
		assert.Equal(t, modelreg.RouteAPI, u.Route)
		assert.Empty(t, u.Passed)
	}
}
