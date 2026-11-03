package mmview

import (
	"context"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

// PlatformWarningFor is what the easy tier of the generation test asks about a model (Issue #977): the model catalog's OS / GPU generation
// columns (Issue #979) say this PC is outside the model's target ("" when it is inside, or the model has no such column).
func PlatformWarningFor(s *modelmanager.Service) func(ctx context.Context, modelID string) string {
	return func(ctx context.Context, modelID string) string {
		for _, v := range s.Catalog() {
			if slices.Contains(v.ModelIDs, modelID) {
				return PlatformWarning(ctx, s, v)
			}
		}
		return ""
	}
}
