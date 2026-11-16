package llm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/service/llmassign"
	"github.com/ousiassllc/mam/backend/internal/service/llmmodels"
)

// defaultView is the assignment the program ships: the Agents of llm.Agents() with the default routes, the two that no code calls marked unused.
func defaultView() llmassign.View {
	var v llmassign.View
	def := llm.DefaultConfig()
	for _, a := range llm.Agents() {
		r := llmassign.Route{
			Primary:  llmassign.Target{Provider: string(def.Routes[a].Primary.Provider), Model: def.Routes[a].Primary.Model},
			Fallback: llmassign.Target{Provider: string(def.Routes[a].Fallback.Provider), Model: def.Routes[a].Fallback.Model},
		}
		v.Entries = append(v.Entries, llmassign.Entry{Agent: string(a), Label: string(a), Unused: a == llm.AgentOrchestrator || a == llm.AgentFinalQC, Route: r, Default: r})
	}
	return v
}

func modelsWithKeys(t *testing.T, providers ...string) *llmmodels.Service {
	t.Helper()
	store := credentialtest.New()
	for _, p := range providers {
		require.NoError(t, store.Set(p, "secret"))
	}
	return llmmodels.New(llmmodels.Options{Keys: store})
}

// Only the OpenAI key is registered: of the 12 Agents that run, 5 have neither model usable (their Anthropic and Google keys are missing) and 4 run on their alternative;
// the Agents that no code calls are not counted, and each missing key is one line, the biggest shortage first.
func TestKeyBanner_CountsTheAgentsThatRunAndGroupsTheMissingKeys(t *testing.T) {
	b, ok := keyBanner(defaultView(), modelsWithKeys(t, "openai"))

	require.True(t, ok)
	assert.Equal(t, 12, b.Rows)
	assert.Equal(t, 5, b.Blocked)
	assert.Equal(t, 4, b.Covered)
	require.Len(t, b.Shortages, 2)
	assert.Equal(t, "Anthropic（Claude）", b.Shortages[0].Provider)
	assert.Len(t, b.Shortages[0].Agents, 10)
	assert.Equal(t, "Google（Gemini）", b.Shortages[1].Provider)
	assert.Len(t, b.Shortages[1].Agents, 7)
	assert.Equal(t, "/settings/api-keys", b.Route)
}

// With every key registered nothing is said.
func TestKeyBanner_IsAbsentWhenNoAgentIsBlockedOrCovered(t *testing.T) {
	_, ok := keyBanner(defaultView(), modelsWithKeys(t, "openai", "anthropic", "google"))

	assert.False(t, ok)
}

// An Agent whose primary runs is neither blocked nor covered, whatever its alternative lacks, and a row names the missing key under the target and says that it runs
// on the alternative.
func TestRowOf_ChipsTheTargetWithoutKeyAndSaysItRunsOnTheAlternative(t *testing.T) {
	models := modelsWithKeys(t, "openai")
	byAgent := map[string]llmassign.Entry{}
	for _, e := range defaultView().Entries {
		byAgent[e.Agent] = e
	}

	producer := rowOf(byAgent["producer"], models) // gpt (key), claude (no key)
	assert.Empty(t, producer.PrimaryChip.Label)
	assert.Equal(t, "キー未登録", producer.FallbackChip.Label)
	assert.Empty(t, producer.Note, "the primary runs")

	artDirector := rowOf(byAgent["art_director"], models) // gemini (no key), gpt (key)
	assert.Equal(t, "キー未登録", artDirector.PrimaryChip.Label)
	assert.Empty(t, artDirector.FallbackChip.Label)
	assert.Contains(t, artDirector.Note, "代替で動きます")
	assert.Contains(t, artDirector.Note, byAgent["art_director"].Route.Fallback.Model)

	director := rowOf(byAgent["director"], models) // neither
	assert.Equal(t, "キー未登録", director.PrimaryChip.Label)
	assert.Equal(t, "キー未登録", director.FallbackChip.Label)
	assert.Empty(t, director.Note, "nothing of it runs")

	finalQC := rowOf(byAgent["final_qc"], models)
	assert.Empty(t, finalQC.PrimaryChip.Label, "an Agent that no code calls says nothing about keys")
}

func TestShortName_DropsTheWordsInBrackets(t *testing.T) {
	assert.Equal(t, "Producer", shortName("Producer（コンセプト）"))
	assert.Equal(t, "Writer", shortName("Writer"))
}
