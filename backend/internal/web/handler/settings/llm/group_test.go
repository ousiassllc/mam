package llm

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/service/llmmodels"
)

func warn(agent, role, provider, why string) llmmodels.Warning {
	return llmmodels.Warning{
		Target:       llmmodels.Target{Agent: agent, Role: role, Provider: provider},
		Availability: modelfilter.Availability{Message: why, Route: "/settings/api-keys"},
	}
}

// The routes that have one cause (the same provider, the same reason) are one line, in the order the causes first appear; another reason for the same provider is a line of its own.
func TestGroupWarnings_OneLineForEachCauseWithTheRoutesThatNameIt(t *testing.T) {
	got := groupWarnings([]llmmodels.Warning{
		warn("Writer", "primary", "openai", "API キーが登録されていません"),
		warn("Writer", "fallback", "anthropic", "API キーが登録されていません"),
		warn("Director", "primary", "openai", "API キーが登録されていません"),
		warn("Editor", "primary", "openai", "ログインしていません"),
	})

	assert.Len(t, got, 3)
	assert.Equal(t, "openai", got[0].Provider)
	assert.Equal(t, []string{"Writerのプライマリ", "Directorのプライマリ"}, got[0].Uses)
	assert.Equal(t, []string{"Writerの代替"}, got[1].Uses)
	assert.Equal(t, "ログインしていません", got[2].Reason)
	assert.Equal(t, "/settings/api-keys", got[0].Route)
}
