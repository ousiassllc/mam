package modelreg_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain/modelreg"
)

func TestRouteOf_ReadsTheRouteFromTheIdentifier(t *testing.T) {
	cases := map[string]modelreg.RouteKind{
		"gpt-image-2.6-flare":                      modelreg.RouteAPI,
		"seedance-2.0":                             modelreg.RouteAPI,
		"gpt-image-2.6-flare:chatgpt-subscription": modelreg.RouteSubscription,
		"gpt-image-2:chatgpt-subscription":         modelreg.RouteSubscription,
		"comfyui:bundled-comfyui/wan22-i2v-14b":    modelreg.RouteLocal,
		"openai-compat:box/flux":                   modelreg.RouteLocal,
		"comfy:abcdefgh1@v2":                       modelreg.RouteLocal,
	}
	for id, want := range cases {
		assert.Equal(t, want, modelreg.RouteOf(id), id)
	}
}

func TestUse_NilReceiverSaysNothing(t *testing.T) {
	var u *modelreg.Use
	assert.Empty(t, u.Suffix())
	assert.Empty(t, u.Explain())
	assert.Equal(t, "（API・従量課金）", (&modelreg.Use{Route: modelreg.RouteAPI, CostBasis: modelreg.CostPriced}).Suffix())
}

func TestUse_ALabelSaysFreeAndUnknownApart(t *testing.T) {
	assert.Equal(t, "ローカル・無料", modelreg.Use{Route: modelreg.RouteLocal, CostBasis: modelreg.CostFree}.Label())
	assert.Equal(t, "ChatGPT サブスク・単価不明", modelreg.Use{Route: modelreg.RouteSubscription, CostBasis: modelreg.CostUnknown}.Label())
	assert.Equal(t, "ローカル・無料・条件に合うモデルを自動選択", modelreg.Use{Route: modelreg.RouteLocal, CostBasis: modelreg.CostFree, Selection: modelreg.SelectionCondition}.Label())
}
