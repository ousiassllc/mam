package mmview

import "github.com/ousiassllc/mam/backend/internal/service/modelmanager"

// Adult is the predicate "this model is an adult one" (the model manager's rating adult, or its uncensored trait) that keeps such a model from being chosen on
// its own, whatever the age confirmation says (modelroute). It is false for a model the catalog does not know.
func Adult(s *modelmanager.Service) func(modelID string) bool {
	info := InfoOf(s)
	return func(modelID string) bool {
		i, ok := info(modelID)
		return ok && (i.Adult || i.Traits.Uncensored)
	}
}
