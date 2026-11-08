package modelroutetest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// The progress card names the model the Adapter runs first. When the primary of the assignment cannot be used and the alternative can, the Adapter moves on
// to the alternative by itself (the assignment is "usable"), so the card must name the alternative, not the primary that will not be called.
func TestCard_NamesTheAlternativeWhenThePrimaryOfTheAssignmentCannotBeUsed(t *testing.T) {
	r := newRig(t, nil)
	// new_reference: GPT Image 2.6 Flare (primary) cannot be used, Nano Banana (alternative) can. Balanced video: Seedance 2.0 (primary) cannot, Kling 3.0 can.
	r.usable = only(domain.ModelNanoBanana, domain.ModelKling3)

	require.NoError(t, r.runPack(t))
	require.NoError(t, r.runGenerate(t))

	text := r.chat.text()
	assert.Contains(t, text, "new_reference＝"+string(domain.ModelNanoBanana)+"（", "the image the Adapter makes is the alternative's")
	assert.NotContains(t, text, string(domain.ModelGPTImage26Flare)+"（", "the primary that cannot be used is not the model to be used")
	assert.Contains(t, text, "使う動画モデル: "+string(domain.ModelKling3)+"（")
	assert.NotContains(t, text, "使う動画モデル: "+string(domain.ModelSeedance2))
}
