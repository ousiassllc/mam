package paths

// The categories of the global settings screen (spec/components/settings-screen.md §2). The slug of a category is the
// prefix of the paths that write its settings, apart from the API keys, whose writes stay at Credential.
const (
	SettingsOverview    = ""
	SettingsAPIKeys     = "api-keys"
	SettingsLLM         = "llm"
	SettingsModels      = "models"
	SettingsLocalModels = "local-models"
	SettingsMCP         = "mcp"
	SettingsUpdate      = "update"
)

// SettingsCategory is the page of a category of the settings screen: "/settings/<slug>", and "/settings" for the
// overview (SettingsOverview, the empty slug).
func SettingsCategory(slug string) string {
	if slug == SettingsOverview {
		return Settings()
	}
	return join(Settings(), slug)
}

// CredentialEdit is "/settings/credentials/<provider>/edit": GET answers the body of the modal that sets the API key of
// the provider.
func CredentialEdit(provider string) string { return join(Settings(), "credentials", provider, "edit") }

// CredentialTest is "/settings/credentials/<provider>/test": POST tries the key of the modal (or the saved one) against
// the provider without saving it.
func CredentialTest(provider string) string { return join(Settings(), "credentials", provider, "test") }

// LocalModelsHealth is "/settings/local-models/health": GET answers the fragment that says how many of the registered
// endpoints answer (the card of the overview reads it after the page is shown).
func LocalModelsHealth() string { return LocalModels() + "/health" }
