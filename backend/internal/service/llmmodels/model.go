package llmmodels

import (
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
)

// Model is one model to offer. Name is empty when the provider gave none (the candidates of mam have none). The rest is what mam knows of a model of the servers it manages
// (llama.cpp, Ollama; #1235): whether its refusals were removed, its license, which server it is of, and, when the model cannot be chosen now whatever its provider says, why.
type Model struct {
	ID   string
	Name string
	// Uncensored: an abliterated derivative (the picker says it is for adults and can filter by it).
	Uncensored bool
	// License is the license of the model with the answer for commercial use (the zero value: not known, as for a model of a vendor's API).
	License modellicense.Info
	// Server is the server of mam the model belongs to ("" for any other).
	Server string
	// Unavailable is why this model cannot be chosen now, with the screen that fixes it (nil: as usable as its provider).
	Unavailable *modelfilter.Availability
}
