// Package chatgptid names the image models that are made with a ChatGPT subscription through the unofficial
// Codex path (spec/architecture/chatgpt-signin.md §5). It is a leaf so that the Model Adapter, the config
// check and the backend agree on the identifiers without importing each other.
package chatgptid

import "github.com/ousiassllc/mam/backend/internal/domain"

const (
	// Model is the identifier of GPT Image 2 in routes, locks and the ledger: "<model>:<how it is reached>".
	Model domain.ModelName = "gpt-image-2:chatgpt-subscription"
	// Flare is GPT Image 2.6 Flare reached the same way. It is a separate identifier (not a setting of Model), so that the model of a
	// result, a lock and a ledger row says which one drew it.
	Flare domain.ModelName = "gpt-image-2.6-flare:chatgpt-subscription"
	// Sunburst is GPT Image 2.6 Sunburst reached the same way (see Flare).
	Sunburst domain.ModelName = "gpt-image-2.6-sunburst:chatgpt-subscription"
	// Label is the display name of Model in the generation test (the model list and the estimate): the model and how it is
	// reached, like the "<name> (v2)" of a saved workflow. It is published with the model (localbackends.Options.Publish).
	Label = "GPT Image 2 (ChatGPT サブスク)"
	// Vendor keys the circuit breaker, the concurrency limit and the timeout of the models.
	Vendor = "chatgpt-subscription"
	// APIModel is the model the Codex endpoint is asked for by Model.
	APIModel = "gpt-image-2"
	// BaseURL is shown in the vendor settings; the backend owns the real endpoint.
	BaseURL = "https://chatgpt.com"
	// TimeoutSeconds bounds one call: the backend gives up after 180 s without a byte (stall) first.
	TimeoutSeconds = 600
)

// Entry is one image model of the subscription: its identifier, the model the Codex endpoint is asked for and its label.
type Entry struct {
	Model    domain.ModelName
	APIModel string
	Label    string
}

// Entries lists the image models of the subscription: the API model of each is its own (a model is never answered by another).
func Entries() []Entry {
	return []Entry{
		{Flare, "gpt-image-2.6-flare", "GPT Image 2.6 Flare (ChatGPT サブスク)"},
		{Sunburst, "gpt-image-2.6-sunburst", "GPT Image 2.6 Sunburst (ChatGPT サブスク)"},
		{Model, APIModel, Label},
	}
}

// Is reports whether m is an image model of the subscription.
func Is(m domain.ModelName) bool { return m == Model || m == Flare || m == Sunburst }
