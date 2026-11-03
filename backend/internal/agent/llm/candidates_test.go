package llm_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
)

func TestCandidateModels_EveryProviderHasSome(t *testing.T) {
	for _, p := range llm.Providers() {
		ids := llm.CandidateModels(p)
		if p == llm.ProviderLocal {
			assert.Empty(t, ids, "the models of a local server are the ones it lists: mam ships none")
			continue
		}
		require.NotEmpty(t, ids, p)
		assert.Len(t, ids, len(uniq(ids)), "%s: an id is listed once", p)
	}
	assert.Nil(t, llm.CandidateModels("nobody"))
}

// The candidates must agree with what the program ships: every model of the default routes is one of the candidates of its
// provider (a default that the picker does not offer would be flagged "may be unusable" on a fresh install).
func TestCandidateModels_ContainTheDefaultRoutes(t *testing.T) {
	for agent, r := range llm.DefaultConfig().Routes {
		for _, target := range []llm.Target{r.Primary, r.Fallback} {
			assert.Contains(t, llm.CandidateModels(target.Provider), target.Model, "%s: %s", agent, target.Provider)
		}
	}
}

// A caller that sorts or edits the answer does not change what the next one sees.
func TestCandidateModels_AreACopy(t *testing.T) {
	first := llm.CandidateModels(llm.ProviderOpenAI)
	first[0] = "changed"
	assert.NotEqual(t, "changed", llm.CandidateModels(llm.ProviderOpenAI)[0])
}

func uniq(ids []string) map[string]bool {
	m := map[string]bool{}
	for _, id := range ids {
		m[id] = true
	}
	return m
}
