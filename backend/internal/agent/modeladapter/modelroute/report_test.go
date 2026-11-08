package modelroute_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/chatgptid"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/modelroute"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/modelreg"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
)

func reportOf(t *testing.T, env modelroute.Env, media, key string) modelroute.RouteReport {
	t.Helper()
	for _, r := range env.Report() {
		if r.Media == media && r.Key == key {
			return r
		}
	}
	require.Failf(t, "no report", "%s %s", media, key)
	return modelroute.RouteReport{}
}

func TestReport_SaysWhatAStartWouldRunOnForEveryRoute(t *testing.T) {
	env := observed(t)

	reports := env.Report()

	assert.Len(t, reports, len(modeladapter.ImagePurposes())+len(domain.QualityPolicies()))
	fresh := reportOf(t, env, "image", string(modeladapter.PurposeNewReference))
	assert.Equal(t, modelroute.StatusCondition, fresh.Status)
	assert.False(t, fresh.Explicit)
	require.NotNil(t, fresh.Use)
	assert.Equal(t, localImage, fresh.Use.Model)
	assert.Equal(t, modelreg.RouteLocal, fresh.Use.Route)
	assert.Equal(t, modelreg.CostFree, fresh.Use.Basis, "this PC is free, which is not the same as an unknown price")
	assert.Equal(t, modelreg.SelectionCondition, fresh.Selection)
	assert.Contains(t, fresh.Reason, "条件に合う利用可能なモデルを選びました")
	require.Len(t, fresh.Assigned, 2)
	assert.False(t, fresh.Assigned[0].Available)
	assert.Equal(t, "/settings/api-keys", fresh.Assigned[0].SettingsRoute)
	assert.Equal(t, modelreg.RouteAPI, fresh.Assigned[0].RouteKind)

	video := reportOf(t, env, "video", string(domain.QualityBalanced))
	assert.Equal(t, modelroute.StatusCondition, video.Status)
	require.NotNil(t, video.Use)
	assert.Equal(t, "720p", video.Use.Resolution)
}

func TestReport_AUsableAssignmentIsReportedAsItIs(t *testing.T) {
	publish(t)
	env := modelroute.Env{Config: modeladapter.DefaultConfig()}

	r := reportOf(t, env, "image", string(modeladapter.PurposeNewReference))

	assert.Equal(t, modelroute.StatusAssignment, r.Status)
	assert.Equal(t, modelreg.SelectionAssignment, r.Selection)
	require.NotNil(t, r.Use)
	assert.Equal(t, domain.ModelGPTImage26Flare, r.Use.Model)
	assert.Equal(t, modelreg.RouteAPI, r.Use.Route)
}

func TestReport_ABlockedExplicitRouteListsTheCandidatesAndSaysWhyNothingIsReplaced(t *testing.T) {
	env := observed(t)
	env.Pinned = true

	r := reportOf(t, env, "image", string(modeladapter.PurposeNewReference))

	assert.Equal(t, modelroute.StatusBlocked, r.Status)
	assert.True(t, r.Explicit)
	assert.Nil(t, r.Use)
	assert.Contains(t, models(r.Candidates), localImage)
	assert.Contains(t, r.Message, "起動オプションで固定")
}

func TestReport_AnAssignmentWithAUsableAlternativeNamesTheModelTheAdapterRunsFirst(t *testing.T) {
	publish(t)
	env := modelroute.Env{Config: modeladapter.DefaultConfig(), Check: noAPIKey(domain.ModelGPTImage26Flare)}
	env.Check = func(m domain.ModelName) modelfilter.Availability {
		if m == domain.ModelNanoBanana {
			return modelfilter.Usable()
		}
		return noAPIKey()(m)
	}

	r := reportOf(t, env, "image", string(modeladapter.PurposeNewReference))

	assert.Equal(t, modelroute.StatusAssignment, r.Status)
	require.NotNil(t, r.Use)
	assert.Equal(t, domain.ModelNanoBanana, r.Use.Model, "the primary has no key: the alternative of the assignment is what runs")
}

func TestEffective_NamesTheModelsAStartWouldUse(t *testing.T) {
	env := observed(t)

	cfg := env.Effective()

	assert.Equal(t, localImage, cfg.ImageRouteFor(modeladapter.PurposeNewReference).Primary.Model)
	assert.Equal(t, localImage, cfg.ImageRouteFor(modeladapter.PurposeEditReference).Primary.Model)
	assert.Equal(t, localVideo, cfg.Policies[domain.QualityBalanced].Primary.Model)
	assert.Equal(t, domain.ModelGPTImage26Flare, env.Config.Images[modeladapter.PurposeNewReference].Primary.Model, "the running setting is not changed")
}

func TestEffective_AnExplicitRouteStaysAsTheUserSetIt(t *testing.T) {
	env := observed(t)
	env.Pinned = true

	cfg := env.Effective()

	assert.Equal(t, env.Config.Images, cfg.Images)
	assert.Equal(t, env.Config.Policies, cfg.Policies)
}

func TestBasisOf_DistinguishesFreeFromUnknownFromPriced(t *testing.T) {
	cfg := localConfig()
	local := cfg.LocalModels["bundled-comfyui/flux-t2i"]
	local.CostPerImage = 0.01
	cfg.LocalModels["bundled-comfyui/flux-t2i"] = local

	assert.Equal(t, modelreg.CostFree, modelroute.BasisOf(cfg, localText, "image"), "a model on this PC is free: 0 USD")
	assert.Equal(t, modelreg.CostPriced, modelroute.BasisOf(cfg, localImage, "image"), "unless the user priced it")
	assert.Equal(t, modelreg.CostUnknown, modelroute.BasisOf(cfg, chatgptid.Flare, "image"), "the plan pays: no unit price, not 0 and not counted")
	assert.Equal(t, modelreg.CostPriced, modelroute.BasisOf(cfg, domain.ModelNanoBanana, "image"))
	assert.Equal(t, modelreg.CostUnknown, modelroute.BasisOf(cfg, domain.ModelGPTImage26Flare, "image"), "an API model without a price in the settings")
	assert.Equal(t, modelreg.CostFree, modelroute.BasisOf(cfg, localVideo, "video"))
	assert.Equal(t, modelreg.CostPriced, modelroute.BasisOf(cfg, domain.ModelSeedance2, "video"))
}

func TestUse_RecordsTheRouteTheCostBasisAndWhy(t *testing.T) {
	env := observed(t)
	pick, err := env.Image(modeladapter.PurposeNewReference)
	require.NoError(t, err)

	chosen := pick.Use(env.Config, localImage, "image", false)
	assert.Equal(t, modelreg.Use{
		Model: string(localImage), Route: modelreg.RouteLocal, Selection: modelreg.SelectionCondition, Reason: pick.Reason,
		Passed:    []modelreg.Passed{{Model: string(domain.ModelGPTImage26Flare), Why: pick.Passed[0].Message}, {Model: string(domain.ModelNanoBanana), Why: pick.Passed[1].Message}},
		CostBasis: modelreg.CostFree,
	}, chosen)
	assert.Contains(t, chosen.Label(), "ローカル・無料・条件に合うモデルを自動選択")

	var assigned modelroute.Selected
	use := assigned.Use(env.Config, domain.ModelNanoBanana, "image", true)
	assert.Equal(t, modelreg.SelectionAssignment, use.Selection)
	assert.Contains(t, use.Reason, "予備のモデル", "the Adapter's own fail-over to the alternative is told")
	assert.Equal(t, "API・従量課金", use.Label())
	assert.Equal(t, "ChatGPT サブスク・単価不明", assigned.Use(env.Config, chatgptid.Flare, "image", false).Label())
}
