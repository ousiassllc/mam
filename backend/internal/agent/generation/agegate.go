package generation

import (
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// checkPackModels refuses a Reference Pack whose image routes name an adult model while the age confirmation is off
// (*agegate.RefusedError), before the job is queued: the pack makes images for both purposes, and the Model Adapter refuses
// the route anyway when the job calls it (modeladapter.Adapter.GenerateImage).
func (o *Orchestrator) checkPackModels() error {
	cfg := o.adapter.Config()
	var models []domain.ModelName
	for _, p := range []modeladapter.ImagePurpose{modeladapter.PurposeNewReference, modeladapter.PurposeEditReference} {
		r := cfg.ImageRouteFor(p)
		models = append(models, r.Primary.Model, r.Fallback.Model)
	}
	return o.adapter.Gate.Check(models...)
}
