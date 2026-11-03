package facefusion

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultsAreTheGhostYunetGfpganCombinationWithoutWarnings(t *testing.T) {
	sel := Selection{}.WithDefaults()
	assert.Equal(t, Selection{Swapper: "ghost_2_256", Detector: "yunet", Enhancer: "gfpgan_1.4"}, sel)

	w, err := Selection{}.Warnings()
	require.NoError(t, err)
	assert.Empty(t, w, "the defaults are the models the FaceFusion list clears (GHOST is conditional, with a note, not a warning)")
}

func TestEveryDefaultIsInItsList(t *testing.T) {
	for kind, list := range map[string][]Model{DefaultSwapper: Swappers, DefaultDetector: Detectors, DefaultEnhancer: Enhancers} {
		assert.True(t, slices.ContainsFunc(list, func(m Model) bool { return m.ID == kind }), kind)
	}
}

func TestNonCommercialResearchAndUnknownModelsWarn(t *testing.T) {
	for id, want := range map[string]string{
		"inswapper_128": "商用NG", "simswap_256": "商用NG", "hyperswap_1a_256": "研究用のみ", "hififace_unofficial_256": "商用不明", "uniface_256": "商用不明",
	} {
		w, err := Selection{Swapper: id}.Warnings()
		require.NoError(t, err, id)
		require.Len(t, w, 1, id)
		assert.Contains(t, w[0], id)
		assert.Contains(t, w[0], want)
	}
	w, err := Selection{Detector: "retinaface", Enhancer: "codeformer"}.Warnings()
	require.NoError(t, err)
	assert.Len(t, w, 2, "one for the detector and one for the enhancer")
	w, err = Selection{Swapper: "inswapper_128", Enhancer: EnhancerNone}.Warnings()
	require.NoError(t, err)
	assert.Len(t, w, 1, "no enhancer, no warning for it")
}

func TestConditionalLicensesCarryANoteButNoWarning(t *testing.T) {
	for _, id := range []string{"ghost_1_256", "ghost_2_256", "ghost_3_256"} {
		i := slices.IndexFunc(Swappers, func(m Model) bool { return m.ID == id })
		require.GreaterOrEqual(t, i, 0)
		assert.False(t, Swappers[i].Warn())
		assert.Contains(t, Swappers[i].Note, "CrossFace")
	}
}

func TestResolveListsTheModelsThatExist(t *testing.T) {
	_, _, _, err := Selection{Enhancer: "nope"}.Resolve()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gfpgan_1.4")
	assert.Contains(t, err.Error(), EnhancerNone)

	_, _, _, err = Selection{Detector: "nope"}.Resolve()
	assert.ErrorContains(t, err, "yunet")
}

func TestModelIDsAreUnique(t *testing.T) {
	for _, list := range [][]Model{Swappers, Detectors, Enhancers} {
		seen := map[string]bool{}
		for _, m := range list {
			assert.False(t, seen[m.ID], m.ID)
			assert.NotEmpty(t, m.License, m.ID)
			seen[m.ID] = true
		}
	}
}
