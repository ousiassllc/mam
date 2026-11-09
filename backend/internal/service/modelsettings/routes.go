package modelsettings

import (
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/agegate"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/modelroute"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// RouteReport is what a generation would run on for one route of the assignment if it started now (modelroute.RouteReport): which of the assigned models can be
// used, the model that would be used and why, and the usable candidates of a route that is blocked.
type RouteReport = modelroute.RouteReport

// SetAdult registers the predicate "this model is an adult one" (mmview.Adult: the rating and everything the age gate treats as adult): such a model is never a candidate of View.Routes, as it is never
// chosen on its own by a generation. nil (the default) knows none. It must be set before the service is used.
func (s *Service) SetAdult(adult func(modelID string) bool) { s.adult = adult }

// routes reports every route of the saved assignment by the rule a generation chooses its models with (modelroute.Env.Report), over the judgement of
// SetAvailability, the age gate of SetHidden and the pin of the file: the screen and the MCP tool read the same answer the start would act on.
func (s *Service) routes(cfg modeladapter.Config) []RouteReport {
	return modelroute.Env{
		Config: cfg,
		Check:  s.availabilityOf,
		Adult:  func(m domain.ModelName) bool { return s.adult != nil && s.adult(string(m)) },
		Pinned: s.pinned,
		Refuse: func(models ...domain.ModelName) error {
			for _, m := range models {
				if s.gated(m) {
					return &agegate.RefusedError{Model: m}
				}
			}
			return nil
		},
	}.Report()
}
