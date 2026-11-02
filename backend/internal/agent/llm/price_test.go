package llm_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
)

func TestPriceCostIsPerMillionTokensOfInputAndOutput(t *testing.T) {
	p, ok := llm.PriceOf(llm.Target{Provider: llm.ProviderAnthropic, Model: "claude-opus-4-5"})
	assert.True(t, ok)
	assert.Equal(t, llm.Price{InputPerMTokUSD: 5, OutputPerMTokUSD: 25}, p, "the row of Anthropic's price page")
	assert.InDelta(t, 5+25, p.Cost(llm.Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000}), 1e-9)
	assert.InDelta(t, 0.0125+0.0125, p.Cost(llm.Usage{InputTokens: 2_500, OutputTokens: 500}), 1e-9)
	assert.Zero(t, p.Cost(llm.Usage{}))
}

func TestPriceOfKnowsOnlyTheModelsThatWereCheckedAndNeverGuesses(t *testing.T) {
	for name, target := range map[string]llm.Target{
		"a model id that is not on the page": {Provider: llm.ProviderGoogle, Model: "gemini-3.1-pro"}, // an id only the app used: the API's is -preview
		"a dated snapshot is another id":     {Provider: llm.ProviderAnthropic, Model: "claude-opus-4-5-20251101"},
		"the model of another provider":      {Provider: llm.ProviderOpenAI, Model: "claude-opus-4-5"},
		"the ChatGPT plan is not per token":  {Provider: llm.ProviderChatGPTSubscription, Model: "gpt-5"},
		"the Codex route is not per token":   {Provider: llm.ProviderChatGPTCodex, Model: "gpt-5"},
		"no model":                           {},
	} {
		_, ok := llm.PriceOf(target)
		assert.False(t, ok, name)
	}
}
