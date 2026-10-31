// Package chatgptid names the image model that is made with a ChatGPT subscription through the unofficial
// Codex path (spec/architecture/chatgpt-signin.md §5). It is a leaf so that the Model Adapter, the config
// check and the backend agree on the identifier without importing each other.
package chatgptid

import "github.com/ousiassllc/mam/backend/internal/domain"

const (
	// Model is the identifier of the model in routes, locks and the ledger: "<model>:<how it is reached>".
	Model domain.ModelName = "gpt-image-2:chatgpt-subscription"
	// Label is the display name of Model in the generation test (the model list and the estimate): the model and how it is
	// reached, like the "<name> (v2)" of a saved workflow. It is published with the model (localbackends.Options.Publish).
	Label = "GPT Image 2 (ChatGPT サブスク)"
	// Vendor keys the circuit breaker, the concurrency limit and the timeout of the model.
	Vendor = "chatgpt-subscription"
	// APIModel is the model the Codex endpoint is asked for.
	APIModel = "gpt-image-2"
	// BaseURL is shown in the vendor settings; the backend owns the real endpoint.
	BaseURL = "https://chatgpt.com"
	// TimeoutSeconds bounds one call: the backend gives up after 180 s without a byte (stall) first.
	TimeoutSeconds = 600
)
