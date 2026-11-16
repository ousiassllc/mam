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
// the Agents that no code calls are not counted. Each line is the Agents the key puts back on their primary, the biggest first, so the lines add up to 5 + 4.
func TestKeyBanner_CountsTheAgentsThatRunAndGroupsTheMissingKeys(t *testing.T) {
	b, ok := keyBanner(defaultView(), modelsWithKeys(t, "openai"))

	require.True(t, ok)
	assert.Equal(t, 12, b.Rows)
	assert.Equal(t, 5, b.Blocked)
	assert.Equal(t, 4, b.Covered)
	require.Len(t, b.Shortages, 2)
	assert.Equal(t, "Anthropic（Claude）", b.Shortages[0].Provider)
	assert.Equal(t, []string{"director", "storyboard", "prompt_planner", "repair", "command_interpreter"}, b.Shortages[0].Agents)
	assert.Equal(t, "Google（Gemini）", b.Shortages[1].Provider)
	assert.Equal(t, []string{"art_director", "video_judge", "editor", "state_extractor"}, b.Shortages[1].Agents)
	assert.Equal(t, "/settings/api-keys", b.Route)
	assert.Equal(t, b.Blocked+b.Covered, len(b.Shortages[0].Agents)+len(b.Shortages[1].Agents), "the lines add up to the Agents the title counts")
}

// Only the OpenAI key is registered: Producer and Writer run on it (their alternative is Anthropic, whose key is missing) so no key brings them back to anything: they are in no
// line, and there is no OpenAI line (its key is there).
func TestKeyBanner_AnAgentWhoseAlternativeLacksTheKeyButWhosePrimaryRunsIsNotCounted(t *testing.T) {
	b, ok := keyBanner(defaultView(), modelsWithKeys(t, "openai"))

	require.True(t, ok)
	for _, s := range b.Shortages {
		assert.NotEqual(t, "OpenAI", s.Provider)
		for _, name := range []string{"producer", "writer", "audio_director"} {
			assert.NotContains(t, s.Agents, name)
		}
	}
}

// Only the Anthropic key is registered: Producer, Writer and Audio Director (primary OpenAI) run on their alternative, so the OpenAI line names exactly them; Director and
// the others whose primary is Anthropic run, and their missing Google alternative adds nothing to the Google line.
func TestKeyBanner_ALineNamesOnlyTheAgentsItsKeyPutsBackOnThePrimary(t *testing.T) {
	b, ok := keyBanner(defaultView(), modelsWithKeys(t, "anthropic"))

	require.True(t, ok)
	byProvider := map[string][]string{}
	for _, s := range b.Shortages {
		byProvider[s.Provider] = s.Agents
	}
	assert.NotContains(t, byProvider, "Anthropic（Claude）")
	assert.Equal(t, []string{"producer", "writer", "audio_director"}, byProvider["OpenAI"])
	assert.Equal(t, []string{"art_director", "video_judge", "editor", "state_extractor"}, byProvider["Google（Gemini）"])
	assert.Equal(t, b.Blocked+b.Covered, len(byProvider["OpenAI"])+len(byProvider["Google（Gemini）"]))
}

// An Agent whose primary runs is neither blocked nor covered, so with every primary usable and every alternative lacking its key there is no banner, though keys are missing.
func TestKeyBanner_IsAbsentWhenOnlyAlternativesLackTheirKeys(t *testing.T) {
	v := defaultView()
	for i := range v.Entries {
		v.Entries[i].Route.Primary = llmassign.Target{Provider: "openai", Model: "gpt"}
		v.Entries[i].Route.Fallback = llmassign.Target{Provider: "anthropic", Model: "claude"}
	}

	_, ok := keyBanner(v, modelsWithKeys(t, "openai"))

	assert.False(t, ok)
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
