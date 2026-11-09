package mmview

import "github.com/ousiassllc/mam/backend/internal/service/modelmanager"

// Adult is the predicate "this model is an adult one" that keeps such a model from being chosen on its own, whatever the age confirmation says (modelroute): the
// model manager's rating adult or its uncensored trait, and everything the age gate itself treats as one (IsAdultModel: an NFAA model added from Hugging Face, a
// model or a saved workflow that reads a file only an adult model has).
func Adult(s *modelmanager.Service) func(modelID string) bool {
	info := InfoOf(s)
	return func(modelID string) bool {
		if s.IsAdultModel(modelID) {
			return true
		}
		i, ok := info(modelID)
		return ok && (i.Adult || i.Traits.Uncensored)
	}
}
