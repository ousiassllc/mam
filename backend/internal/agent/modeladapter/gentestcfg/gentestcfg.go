// Package gentestcfg holds the settings of the generation test screen (models.json, key "gentest"): the limits of one batch and
// how the face is mixed into the similarity of the prompt match (spec/architecture/gentest/gentest.md, prompt-match.md). It is a
// package of its own because modeladapter is over its line limit.
package gentestcfg

import (
	"errors"
	"math"

	"github.com/ousiassllc/mam/backend/internal/agent/imagematch"
)

// Defaults of the limits.
const (
	DefaultMaxItems   = 50
	DefaultMaxCostUSD = 20.0
)

// Limits bound one batch: the number of results (every model x prompt x count) and the estimated cost. A key that the file
// leaves out keeps its default. MatchFace is how the face of the generated image is mixed into the similarity of the prompt match
// (#964; the one place for the weight and the mapping, which #960 changes with the face model).
type Limits struct {
	// MaxItems is the most results of one batch (default 50).
	MaxItems *int `json:"max_items,omitempty"`
	// MaxCostUSD is the most the priced results of a batch may be estimated to cost (default 20). 0 is no limit.
	MaxCostUSD *float64 `json:"max_cost_usd,omitempty"`
	// MatchFace is the weight and the mapping of the face points of the prompt match (default: the face is not mixed in).
	MatchFace *imagematch.FaceMix `json:"match_face,omitempty"`
}

// Face is how the face is mixed into the similarity of the prompt match.
func (l Limits) Face() imagematch.FaceMix {
	if l.MatchFace == nil {
		return imagematch.DefaultFaceMix()
	}
	return *l.MatchFace
}

// Items is the limit of results in a batch.
func (l Limits) Items() int {
	if l.MaxItems == nil {
		return DefaultMaxItems
	}
	return *l.MaxItems
}

// CostUSD is the limit of the estimated cost in USD; 0 means there is none.
func (l Limits) CostUSD() float64 {
	if l.MaxCostUSD == nil {
		return DefaultMaxCostUSD
	}
	return *l.MaxCostUSD
}

// Validate refuses a limit that cannot work: no result at all, or a negative or non-finite cost.
func (l Limits) Validate() []error {
	var errs []error
	if l.Items() < 1 {
		errs = append(errs, errors.New("gentest.max_items must be at least 1"))
	}
	if c := l.CostUSD(); c < 0 || math.IsNaN(c) || math.IsInf(c, 0) {
		errs = append(errs, errors.New("gentest.max_cost_usd must be 0 (no limit) or positive"))
	}
	if err := l.Face().Validate(); err != nil {
		errs = append(errs, errors.New("gentest.match_face: "+err.Error()))
	}
	return errs
}
