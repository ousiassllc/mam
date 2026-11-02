package imagematch_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/imagematch"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
)

var (
	opus     = llm.Target{Provider: llm.ProviderAnthropic, Model: "claude-opus-4-5"}
	gpt5mini = llm.Target{Provider: llm.ProviderOpenAI, Model: "gpt-5-mini"}
	plan     = llm.Target{Provider: llm.ProviderChatGPTSubscription, Model: "gpt-5"}
)

func TestPlanNamesTheTargetsAndCountsOneWriteAndOneCompareAround(t *testing.T) {
	write, compare := imagematch.New(&answer{}, opus, gpt5mini).Plan(3)
	assert.Equal(t, opus, write.Target)
	assert.Equal(t, gpt5mini, compare.Target)
	assert.Equal(t, 3, write.Calls, "the first round describes, the others fix: one write a round")
	assert.Equal(t, 3, compare.Calls)
}

func TestPlanPricesThePrimaryTargetsPerTokenAndTheDearestKindOfCall(t *testing.T) {
	write, compare := imagematch.New(&answer{}, opus, gpt5mini).Plan(2)
	require.NotNil(t, write.UnitUSD)
	require.NotNil(t, compare.UnitUSD)
	// Opus 4.5 is $5 / $25 per million tokens: a fix (looks at two pictures and the whole prompt) is the dearest call that writes.
	assert.InDelta(t, (5700*5+2500*25)/1e6, *write.UnitUSD, 1e-9)
	// GPT-5 mini is $0.25 / $2: a comparison looks at two pictures.
	assert.InDelta(t, (3700*0.25+1000*2)/1e6, *compare.UnitUSD, 1e-9)
}

func TestPlanLeavesTheUnitOutWhenThePriceIsNotKnown(t *testing.T) {
	write, compare := imagematch.New(&answer{}, plan, llm.Target{Provider: llm.ProviderGoogle, Model: "gemini-3.1-pro"}).Plan(2)
	assert.Nil(t, write.UnitUSD, "a ChatGPT plan is not billed per call")
	assert.Nil(t, compare.UnitUSD, "a model id that is not on the price page is not priced by guess")
}
