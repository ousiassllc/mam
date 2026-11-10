package genplan_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/genplan"
)

func usd(v float64) *float64 { return &v }

func TestOverrun_IsWhatThePlanNeedsOverTheCeilings(t *testing.T) {
	plan := genplan.Plan{
		Lines: []genplan.Line{
			{VideoCost: genplan.Cost{USD: 2}, PackCost: genplan.Cost{USD: 1}},
			{VideoCost: genplan.Cost{USD: 4}},
		},
		ProjectSpent: 1,
	}

	assert.False(t, plan.Overrun().Any(), "no ceiling, no overrun")

	plan.ProjectCeiling = usd(8)
	assert.False(t, plan.Overrun().Any(), "1 spent + 7 planned fits 8")

	plan.ProjectCeiling = usd(7)
	assert.Equal(t, genplan.Overrun{ProjectNeed: 8}, plan.Overrun(), "the ceiling the plan needs is what is spent plus what it costs")

	plan.ProjectCeiling, plan.ShotCeiling = nil, usd(3.5)
	assert.Equal(t, genplan.Overrun{ShotNeed: 4}, plan.Overrun(), "the dearest Shot decides the Shot ceiling")

	plan.Lines[1].VideoCost = genplan.Cost{Unknown: true}
	assert.False(t, plan.Overrun().Any(), "a price that is not known is not counted, as the ceiling does not count it")
}

// The Reference Pack is charged to its Shot, so what a Shot already holds counts against the Shot ceiling with what the plan adds: a plan under the ceiling
// by its own price still overruns when the Shot has spent (spec/spec.md §4.3).
func TestOverrun_CountsWhatTheShotAlreadyHolds(t *testing.T) {
	plan := genplan.Plan{
		Lines:       []genplan.Line{{ShotID: "s1", VideoCost: genplan.Cost{USD: 2}}, {ShotID: "s2", VideoCost: genplan.Cost{USD: 2}}},
		ShotCeiling: usd(3),
		ShotSpent:   map[string]float64{"s1": 1.5},
	}
	assert.Equal(t, genplan.Overrun{ShotNeed: 3.5}, plan.Overrun(), "1.5 held + 2 planned passes 3, though each plan line alone fits")

	plan.ShotSpent["s1"] = 1
	assert.False(t, plan.Overrun().Any(), "1 held + 2 planned fits 3")
}

func TestUnpricedNotice_OnlyWithABudget(t *testing.T) {
	plan := genplan.Plan{Unpriced: []string{"動画: kling-3.0"}}
	assert.Empty(t, plan.UnpricedNotice(), "no budget: nothing to tell")

	plan.ProjectCeiling = usd(20)
	assert.Contains(t, plan.UnpricedNotice(), "単価不明のモデルが含まれます")
	assert.Contains(t, plan.UnpricedNotice(), "動画: kling-3.0")

	plan.Unpriced = nil
	assert.Empty(t, plan.UnpricedNotice(), "every price is known")
}
