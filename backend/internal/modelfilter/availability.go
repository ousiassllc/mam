package modelfilter

import "github.com/ousiassllc/mam/backend/internal/web/paths"

// Reason says why a model cannot be chosen now (#1013). It is a code; Availability.Message is the sentence for the user.
type Reason string

// The reasons. no_backend, circuit_open and missing_node are the codes the generation test already used (domain.ExcludedNoBackend, domain.ExcludedCircuitOpen,
// modelcatalog.ExcludedMissingNode); their values do not change. The adapter's reason for the age gate is a sentence (agegate.Excluded): the lists give age_gate for it.
const (
	ReasonNoKey        Reason = "no_backend"           // an API provider without a saved API key (the model has no backend yet)
	ReasonCircuitOpen  Reason = "circuit_open"         // the vendor failed repeatedly and is paused for a while
	ReasonProvider     Reason = "provider_unsupported" // the selected API provider (OpenRouter, fal.ai, Replicate) does not serve the model (domain.ExcludedProvider); another provider or the official one does
	ReasonAgeGate      Reason = "age_gate"             // an adult model while the age confirmation is off
	ReasonMissingNode  Reason = "missing_node"         // a ComfyUI workflow whose custom nodes are not installed
	ReasonNotSignedIn  Reason = "not_signed_in"        // a subscription provider (ChatGPT) without a sign-in, or with image generation off
	ReasonNotInstalled Reason = "not_installed"        // a local model that is not installed (the model manager's catalog)
	ReasonEndpoint     Reason = "endpoint_unusable"    // a registered ComfyUI / OpenAI-compatible endpoint that cannot be used
)

// Where a model comes from, for the route of its reason: the same words as modelcatalog.Source*.
const (
	SourceBuiltin  = "builtin"
	SourceChatGPT  = "chatgpt"
	SourceLocal    = "local"
	SourceWorkflow = "workflow"
	// SourceLocalLLM is the local OpenAI-compatible LLM server (the LLM provider "local", #1008).
	SourceLocalLLM = "local_llm"
)

// ProviderChatGPT is the provider of the ChatGPT subscription's models (the LLM provider "chatgpt-subscription" and the image model made with it).
const ProviderChatGPT = "chatgpt-subscription"

// Availability is whether a model can be chosen now, with the reason and the way to fix it. Every list a screen chooses a model from and the MCP tools
// give it in the same shape (the JSON keys are the ones of the generation test's model list).
type Availability struct {
	Available bool `json:"available"`
	// Reason is the code (a Reason); empty while Available.
	Reason Reason `json:"unavailable,omitempty"`
	// Message is the reason for the user ("API キーが登録されていません（設定の「API キー」）").
	Message string `json:"unavailable_message,omitempty"`
	// Route is the screen that fixes it, as a path of the app ("/settings/api-keys"); empty when none helps (the circuit breaker closes by itself).
	Route string `json:"settings_route,omitempty"`
}

// Usable is the availability of a model that can be chosen.
func Usable() Availability { return Availability{Available: true} }

// Unusable is the availability of a model that cannot be chosen now: its reason code, the message for the user, and the route that fixes it (RouteOf).
func Unusable(reason Reason, message, route string) Availability {
	return Availability{Reason: reason, Message: message, Route: route}
}

// RouteOf is the screen that fixes a reason for a model of the source (the Source* words): the API key and the ChatGPT sign-in are on the settings
// screen's "API キーとログイン" (a subscription model is the sign-in's section, but the same page), a local endpoint on "ローカル・代替モデル" (the local LLM server on the model manager's page "ローカル LLM", #1104), an uninstalled
// or gated local model in the model manager's page of the image and video models (a missing node: the page of the custom nodes). A reason that fixes itself (the circuit breaker) has no route.
func RouteOf(reason Reason, source string) string {
	switch reason {
	case ReasonCircuitOpen:
		return ""
	case ReasonAgeGate, ReasonNotInstalled:
		return paths.ModelManagerCategory(paths.MMImageVideo)
	case ReasonMissingNode:
		return paths.ModelManagerCategory(paths.MMCustomNodes)
	case ReasonEndpoint:
		if source == SourceLocalLLM {
			return paths.ModelManagerCategory(paths.MMLocalLLM)
		}
		return paths.SettingsCategory(paths.SettingsLocalModels)
	case ReasonNotSignedIn, ReasonProvider: // the provider is chosen at the top of the API key category
		return paths.SettingsCategory(paths.SettingsAPIKeys)
	case ReasonNoKey:
		if source == SourceLocal || source == SourceWorkflow {
			return paths.SettingsCategory(paths.SettingsLocalModels)
		}
		return paths.SettingsCategory(paths.SettingsAPIKeys)
	}
	return ""
}

// Item fills the availability of an Item.
func (a Availability) Item(it Item) Item {
	it.Available, it.Reason = a.Available, a.Message
	return it
}
