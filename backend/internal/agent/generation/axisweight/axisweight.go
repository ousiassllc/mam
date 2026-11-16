// Package axisweight is the weighting of the four score axes by the nature of a Shot (spec/architecture/model-adapters-m3.md §12.3): Multi-model routing (package generation)
// weighs the Judge record of a video model with it.
package axisweight

import (
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Of are the weights of the four score axes the nature of the Shot gives (§12.3): every axis starts at 1 and the ones the Shot depends on gain 2. They sum to 1.
func Of(spec *domain.ShotSpec) domain.TakeScores {
	w := domain.TakeScores{Product: 1, Acting: 1, Camera: 1, Continuity: 1}
	if spec != nil {
		if spec.Product != nil {
			w.Product += 2
		}
		if spec.Character != nil || len(spec.Acting) > 0 {
			w.Acting += 2
		}
		if mv := strings.TrimSpace(spec.Camera.Movement); mv != "" && !strings.EqualFold(mv, "static") {
			w.Camera += 2
		}
		if spec.Continuity.StartFromPreviousFinalState || spec.Lighting.Inherit != "" {
			w.Continuity += 2
		}
	}
	sum := w.Product + w.Acting + w.Camera + w.Continuity
	return domain.TakeScores{Product: w.Product / sum, Acting: w.Acting / sum, Camera: w.Camera / sum, Continuity: w.Continuity / sum}
}

// Score is the mean of the axis scores weighted by w (0-100).
func Score(scores, w domain.TakeScores) float64 {
	return scores.Product*w.Product + scores.Acting*w.Acting + scores.Camera*w.Camera + scores.Continuity*w.Continuity
}
