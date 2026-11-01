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
	SettingsFFmpeg      = "ffmpeg"
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

// LLMAgent is "/settings/llm/agents/<agent>": PUT saves the LLM assignment of one Agent (the modal's form) and DELETE puts
// it back to the default.
func LLMAgent(agent string) string { return join(LLMSettings(), "agents", agent) }

// LLMAgentEdit is "/settings/llm/agents/<agent>/edit": GET answers the body of the modal that edits the Agent.
func LLMAgentEdit(agent string) string { return join(LLMSettings(), "agents", agent, "edit") }

// ModelImage is "/settings/models/images/<purpose>": PUT saves the models of one image purpose.
func ModelImage(purpose string) string { return join(ModelSettings(), "images", purpose) }

// ModelImageEdit is "/settings/models/images/<purpose>/edit": GET answers the body of the modal that edits the purpose.
func ModelImageEdit(purpose string) string { return join(ModelSettings(), "images", purpose, "edit") }

// ModelPolicy is "/settings/models/policies/<policy>": PUT saves the video models and the Takes of one Quality Policy.
func ModelPolicy(policy string) string { return join(ModelSettings(), "policies", policy) }

// ModelPolicyEdit is "/settings/models/policies/<policy>/edit": GET answers the body of the modal that edits the policy.
func ModelPolicyEdit(policy string) string { return join(ModelSettings(), "policies", policy, "edit") }
