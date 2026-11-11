package genplan_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/genplan"
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
