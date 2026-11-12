package genplan_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/genplan"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/modelroute"
	"github.com/ousiassllc/mam/backend/internal/domain/modelreg"
)

func TestUSDTextWritesAnAmountTheWayTheSettingsScreenDoes(t *testing.T) {
	for want, v := range map[string]float64{"3": 3, "12.5": 12.5, "0.1272": 0.1272, "6.75": 6.75, "0": 0, "0.15": 0.15000000000000002} {
		assert.Equal(t, want, genplan.USDText(v))
	}
}

func TestAnnounceText_AnUnpricedModelWithACeilingIsToldOnceNotThreeTimes(t *testing.T) {
	plan := genplan.Plan{
		Lines:          []genplan.Line{{ShotID: "s1", Number: 1}},
		Unpriced:       []string{"動画: kling-3.0"},
		ProjectCeiling: usd(3),
	}

	text := plan.AnnounceText()

	assert.Equal(t, 1, strings.Count(text, "動画: kling-3.0"), "the model is named once")
	assert.Contains(t, text, "単価不明のモデルが含まれます")
	assert.Equal(t, 1, strings.Count(text, "予算上限に数えられ"), "that the ceiling does not count it is said once")
	assert.Contains(t, text, "単価が分からない分があります。費用の上限は保証できません")
	assert.Contains(t, text, "Project 3 USD", "the ceiling is written as the settings screen does")
	assert.NotContains(t, text, "3.0000")
}

func TestAnnounceText_AnUnpricedModelWithoutACeilingKeepsTheListAndTheRiskLine(t *testing.T) {
	plan := genplan.Plan{Lines: []genplan.Line{{ShotID: "s1", Number: 1}}, Unpriced: []string{"動画: kling-3.0"}}

	text := plan.AnnounceText()

	assert.Equal(t, 1, strings.Count(text, "動画: kling-3.0"))
	assert.Contains(t, text, "費用の上限は保証できません")
}

// The announcement names the route of each model and, for a model that is not the assignment's, which model was passed over and why: a match of
// conditions, never "the best model" (#1112).
func TestAnnounceText_NamesTheRouteOfEachModelAndWhyTheAssignmentWasPassedOver(t *testing.T) {
	unusable := func(m string) modelroute.AssignedModel {
		return modelroute.AssignedModel{Model: m, RouteKind: modelreg.RouteAPI, UnavailableMessage: "API キーが登録されていません"}
	}
	plan := genplan.Plan{
		Lines: []genplan.Line{{ShotID: "s1", Number: 1, Pack: true}},
		Routes: []modelroute.RouteReport{
			{
				Media: "image", Key: "new_reference", Status: modelroute.StatusCondition, Reason: "既定の割当が使えないため、条件に合う利用可能なモデルを選びました",
				Assigned: []modelroute.AssignedModel{unusable("gpt-image-2.6-flare"), unusable("nano-banana")},
				Use:      &modelroute.Candidate{Model: "gpt-image-2.6-flare:chatgpt-subscription", Route: modelreg.RouteSubscription, Basis: modelreg.CostUnknown},
			},
			{
				Media: "video", Key: "balanced", Status: modelroute.StatusAssignment,
				Assigned: []modelroute.AssignedModel{{Model: "seedance-2.0", Available: true}},
				Use:      &modelroute.Candidate{Model: "seedance-2.0", Route: modelreg.RouteAPI, Basis: modelreg.CostPriced},
			},
		},
	}

	text := plan.AnnounceText()

	assert.Contains(t, text, "使うモデルと経路")
	assert.Contains(t, text, "gpt-image-2.6-flare:chatgpt-subscription（"+modelreg.RouteSubscription.Label())
	assert.Contains(t, text, "gpt-image-2.6-flare＝API キーが登録されていません、nano-banana＝API キーが登録されていません", "which models of the assignment were passed over, and why")
	assert.Contains(t, text, "品質で選んだものではありません")
	assert.Contains(t, text, "seedance-2.0（"+modelreg.RouteAPI.Label())
	assert.NotContains(t, text, "最高", "no claim of the best model")
	assert.NotContains(t, text, "おすすめ")
}

func TestAnnounceText_WithoutRoutesNamesNone(t *testing.T) {
	assert.NotContains(t, genplan.Plan{Lines: []genplan.Line{{ShotID: "s1", Number: 1}}}.AnnounceText(), "使うモデルと経路")
}

type reporter struct{}

func (reporter) RouteReports() []modelroute.RouteReport {
	return []modelroute.RouteReport{{Media: "video", Key: "fast"}}
}

func TestRoutesOf_IsTheReportOfAGeneratorThatHasOneAndNilForOneThatHasNot(t *testing.T) {
	assert.Len(t, genplan.RoutesOf(reporter{})(), 1)
	assert.Nil(t, genplan.RoutesOf(struct{}{}))
}
