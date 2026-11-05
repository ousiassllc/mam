package gentest

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/intent"
)

// IntentSource is what the choice of a model by an intent or a recipe asks of the service (intent.Source).
func (s *Service) IntentSource() intent.Source {
	return intent.Source{Models: s.Models, Filter: s.DefaultFilter(), BundledEndpoint: s.BundledEndpoint, PlatformWarning: s.PlatformWarning}
}

// Recommend is the model an intent picks now (intent.Recommend). intentapply.Apply puts it in a request.
func (s *Service) Recommend(ctx context.Context, id string, hasReferences bool) (intent.Recommendation, error) {
	return intent.Recommend(ctx, s.IntentSource(), id, hasReferences)
}
