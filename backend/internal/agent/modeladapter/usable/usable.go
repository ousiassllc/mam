// Package usable holds what the Model Adapter's callers learn about the models only once the application is wired: whether a model can be used now
// (the judgement the generation test lists the models with: an API key, a sign-in, an installed local model), whether it is an adult model, and
// whether the operator pinned the assignment file. modelroute reads it to choose the models of a generation (spec/architecture/model-adapters.md §3.1).
// It is a leaf next to agegate: the Adapter owns one (Adapter.Usable), set after the services are built, and every field applies at once.
package usable

import (
	"sync/atomic"

	"github.com/ousiassllc/mam/backend/internal/modelfilter"
)

// Source is what the application tells the Model Adapter about its models. The zero value knows nothing: every model is as the Adapter itself
// finds it (no backend, no key, breaker), no model is an adult one and the assignment is not pinned.
type Source struct {
	availability atomic.Pointer[func(modelID string) (modelfilter.Availability, bool)]
	adult        atomic.Pointer[func(modelID string) bool]
	pinned       atomic.Bool
}

// SetAvailability installs the judgement of "can this model be used now" (gentest.Service.Availability). The second result is false for a
// model it does not know. nil removes it.
func (s *Source) SetAvailability(f func(modelID string) (modelfilter.Availability, bool)) {
	s.availability.Store(&f)
}

// Availability asks the judgement; false when none is installed or the model is not known to it.
func (s *Source) Availability(modelID string) (modelfilter.Availability, bool) {
	if f := s.availability.Load(); f != nil && *f != nil {
		return (*f)(modelID)
	}
	return modelfilter.Availability{}, false
}

// SetAdult installs the predicate that says a model is an adult one (rated adult or uncensored by the model manager's catalog): such a model is
// never chosen on its own, whatever the age confirmation says. nil removes it.
func (s *Source) SetAdult(f func(modelID string) bool) { s.adult.Store(&f) }

// Adult reports whether the model is an adult one.
func (s *Source) Adult(modelID string) bool {
	f := s.adult.Load()
	return f != nil && *f != nil && (*f)(modelID)
}

// SetPinned records that the operator fixed the assignment file (--model-config): it is theirs, so no model is chosen in place of what it names.
func (s *Source) SetPinned(pinned bool) { s.pinned.Store(pinned) }

// Pinned reports whether the operator fixed the assignment file.
func (s *Source) Pinned() bool { return s.pinned.Load() }
