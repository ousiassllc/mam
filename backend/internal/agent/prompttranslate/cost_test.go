package prompttranslate_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/prompttranslate"
)

func TestUsageOfGrowsWithTheTextsAndHasRoomToReason(t *testing.T) {
	none := prompttranslate.UsageOf(nil)
	assert.Positive(t, none.InputTokens, "the instruction is sent")
	assert.Positive(t, none.OutputTokens, "a model that reasons first")

	small := prompttranslate.UsageOf([]string{"狐"})
	big := prompttranslate.UsageOf([]string{"夕暮れの海辺の狐", "ぼやけ"})
	assert.Greater(t, small.InputTokens, none.InputTokens)
	assert.Greater(t, big.InputTokens, small.InputTokens)
	assert.Greater(t, big.OutputTokens, small.OutputTokens)
}

func TestPlanPricesTheCallAtThePriceOfTheTargetOfTheRoute(t *testing.T) {
	texts := []string{"夕暮れの海辺の狐", "ぼやけ"}
	priced := llm.Target{Provider: llm.ProviderAnthropic, Model: "claude-haiku-4-5"}
	price, ok := llm.PriceOf(priced)
	require.True(t, ok)

	got, usd := prompttranslate.New(&answer{}, priced).Plan(texts)
	assert.Equal(t, priced, got)
	require.NotNil(t, usd)
	assert.InDelta(t, price.Cost(prompttranslate.UsageOf(texts)), *usd, 1e-12)
	assert.Positive(t, *usd)
}

func TestPlanFollowsTheRouteWhenItChanges(t *testing.T) {
	cur := llm.Target{Provider: llm.ProviderAnthropic, Model: "claude-haiku-4-5"}
	tr := prompttranslate.NewFollowing(&answer{}, func() llm.Target { return cur })
	_, a := tr.Plan([]string{"狐"})
	cur = llm.Target{Provider: llm.ProviderAnthropic, Model: "claude-opus-4-5"}
	got, b := tr.Plan([]string{"狐"})
	assert.Equal(t, "claude-opus-4-5", got.Model)
	require.NotNil(t, a)
	require.NotNil(t, b)
	assert.Greater(t, *b, *a, "the saved assignment applies at once")
}

func TestPlanHasNoPriceForATargetWithoutACheckedOne(t *testing.T) {
	for name, tg := range map[string]llm.Target{
		"a ChatGPT plan":           {Provider: llm.ProviderChatGPTSubscription, Model: "gpt-5"},
		"a model not in the table": {Provider: llm.ProviderOpenAI, Model: "not-in-the-table"},
	} {
		got, usd := prompttranslate.New(&answer{}, tg).Plan([]string{"狐"})
		assert.Equal(t, tg, got, name)
		assert.Nil(t, usd, name+": a price that was not checked is never made up")
	}
}

func TestPlanOfALocalModelIsFree(t *testing.T) {
	_, usd := prompttranslate.New(&answer{}, llm.Target{Provider: llm.ProviderLocal, Model: "anything"}).Plan([]string{"狐"})
	require.NotNil(t, usd)
	assert.Zero(t, *usd)
}
