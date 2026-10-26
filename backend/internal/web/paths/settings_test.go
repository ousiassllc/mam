package paths_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// The slug of a category is the prefix of the paths that write its settings (spec/components/settings-screen.md §2),
// so the page and its writes cannot drift apart; the overview is /settings itself.
func TestSettingsCategory_SlugsAreThePrefixOfTheWritePaths(t *testing.T) {
	assert.Equal(t, paths.Settings(), paths.SettingsCategory(paths.SettingsOverview))
	assert.Equal(t, "/settings/api-keys", paths.SettingsCategory(paths.SettingsAPIKeys))
	assert.Equal(t, paths.LLMSettings(), paths.SettingsCategory(paths.SettingsLLM))
	assert.Equal(t, paths.ModelSettings(), paths.SettingsCategory(paths.SettingsModels))
	assert.Equal(t, paths.LocalModels(), paths.SettingsCategory(paths.SettingsLocalModels))
	assert.Equal(t, paths.MCPSettings(), paths.SettingsCategory(paths.SettingsMCP))
	assert.Equal(t, "/settings/update", paths.SettingsCategory(paths.SettingsUpdate))
	assert.Equal(t, "/settings/a%2Fb", paths.SettingsCategory("a/b"))
}

func TestCredentialModalPaths_EscapeTheProvider(t *testing.T) {
	assert.Equal(t, "/settings/credentials/kling/edit", paths.CredentialEdit("kling"))
	assert.Equal(t, "/settings/credentials/kling/test", paths.CredentialTest("kling"))
	assert.Equal(t, "/settings/credentials/a%2Fb/edit", paths.CredentialEdit("a/b"))
}

func TestRowModalPaths_AreUnderTheirCategoryAndEscapeTheKey(t *testing.T) {
	assert.Equal(t, "/settings/llm/agents/writer", paths.LLMAgent("writer"))
	assert.Equal(t, "/settings/llm/agents/a%2Fb/edit", paths.LLMAgentEdit("a/b"))
	assert.Equal(t, "/settings/models/images/preview", paths.ModelImage("preview"))
	assert.Equal(t, "/settings/models/images/preview/edit", paths.ModelImageEdit("preview"))
	assert.Equal(t, "/settings/models/policies/max_quality", paths.ModelPolicy("max_quality"))
	assert.Equal(t, "/settings/models/policies/max_quality/edit", paths.ModelPolicyEdit("max_quality"))
}
