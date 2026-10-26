package modelsettings

import (
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// SetHidden registers the age gate: hidden reports whether the model with this identifier is an adult model while the age
// confirmation is off (modelmanager.Service.HidesModel). Such a model is left out of the choices (View.ImageModels and
// VideoModels) and a route that names one is refused by Save, whether it is new or was saved while the gate was open.
// It is asked on every call, so a change of the age confirmation applies at once. It must be set before the service is used;
// nil (the default) hides nothing.
func (s *Service) SetHidden(hidden func(modelID string) bool) { s.hidden = hidden }

// gated reports whether the model is refused now.
func (s *Service) gated(m domain.ModelName) bool { return s.hidden != nil && s.hidden(string(m)) }

// gatedRoutes lists the gated models that the routes of cfg name, each once, images first.
func (s *Service) gatedRoutes(cfg modeladapter.Config) []domain.ModelName {
	var out []domain.ModelName
	add := func(m domain.ModelName) {
		if s.gated(m) && !slices.Contains(out, m) {
			out = append(out, m)
		}
	}
	for _, p := range modeladapter.ImagePurposes() {
		add(cfg.Images[p].Primary.Model)
		add(cfg.Images[p].Fallback.Model)
	}
	for _, q := range domain.QualityPolicies() {
		add(cfg.Policies[q].Primary.Model)
		add(cfg.Policies[q].Fallback.Model)
	}
	return out
}

// gatedReason is why Save refuses the gated model m.
func gatedReason(m domain.ModelName) string {
	return string(m) + " は成人向けモデルで、年齢確認がオフのため選べません。年齢確認をオンにするか、別のモデルを選んでください"
}
