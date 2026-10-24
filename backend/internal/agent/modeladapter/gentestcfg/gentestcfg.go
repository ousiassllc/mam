// Package gentestcfg holds the limits of one batch of the generation test screen (models.json, key "gentest";
// spec/architecture/gentest/gentest.md). It is a package of its own because modeladapter is over its line limit.
package gentestcfg

import (
	"errors"
	"math"
)

// Defaults of the limits.
const (
	DefaultMaxItems   = 50
	DefaultMaxCostUSD = 20.0
)

// Limits bound one batch: the number of results (every model x prompt x count) and the estimated cost. A key that the file
// leaves out keeps its default.
type Limits struct {
	// MaxItems is the most results of one batch (default 50).
	MaxItems *int `json:"max_items,omitempty"`
	// MaxCostUSD is the most the priced results of a batch may be estimated to cost (default 20). 0 is no limit.
	MaxCostUSD *float64 `json:"max_cost_usd,omitempty"`
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
	return errs
}
