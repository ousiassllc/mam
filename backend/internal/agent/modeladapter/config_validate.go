package modeladapter

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Validate checks that every route names a model that has an adapter and settings, that video
// targets use a resolution the model supports and that the numbers are positive. Entries of a map
// are checked in key order, so the joined error lists the problems in the same order on every call.
func (c Config) Validate() error {
	var errs []error
	for _, check := range []func() []error{
		c.validateVendors, c.validateModels, c.validateImageRoutes,
		c.validatePolicies, c.validateRouting, c.validateRetry,
	} {
		errs = append(errs, check()...)
	}
	return errors.Join(errs...)
}

func (c Config) validateVendors() []error {
	var errs []error
	for _, v := range slices.Sorted(maps.Keys(c.Vendors)) {
		vc := c.Vendors[v]
		if !slices.Contains(Vendors(), v) {
			errs = append(errs, fmt.Errorf("vendor %q is unknown", v))
		}
		if vc.BaseURL == "" || vc.MaxConcurrency <= 0 || vc.TimeoutSeconds <= 0 {
			errs = append(errs, fmt.Errorf("vendor %s: base_url, max_concurrency and timeout_seconds must be set", v))
		}
	}
	return errs
}

// validateModels also catches a typo in the key of a map entry, which would otherwise change
// nothing without a word.
func (c Config) validateModels() []error {
	var errs []error
	for _, m := range slices.Sorted(maps.Keys(c.Models)) {
		caps, video := Capabilities(m)
		if !slices.Contains(domain.ImageModels(), m) && !video {
			errs = append(errs, fmt.Errorf("models: %q is not an image or video model", m))
		}
		for _, res := range slices.Sorted(maps.Keys(c.Models[m].CostPerSecond)) {
			if video && !caps.SupportsResolution(res) {
				errs = append(errs, fmt.Errorf("models %s: cost_per_second_usd: %q is not a resolution of the model", m, res))
			}
		}
	}
	return errs
}

func (c Config) validateImageRoutes() []error {
	var errs []error
	for _, p := range slices.Sorted(maps.Keys(c.Images)) {
		if !slices.Contains(ImagePurposes(), p) {
			errs = append(errs, fmt.Errorf("images: %q is not an image purpose", p))
		}
	}
	for _, p := range ImagePurposes() {
		r, ok := c.Images[p]
		if !ok {
			errs = append(errs, fmt.Errorf("images: no route for %s", p))
			continue
		}
		errs = append(errs, c.checkImageTarget("images "+string(p)+" primary", r.Primary)...)
		errs = append(errs, c.checkImageTarget("images "+string(p)+" fallback", r.Fallback)...)
	}
	return errs
}

func (c Config) validatePolicies() []error {
	var errs []error
	for _, q := range slices.Sorted(maps.Keys(c.Policies)) {
		if !q.Valid() {
			errs = append(errs, fmt.Errorf("policies: %q is not a Quality Policy", q))
		}
	}
	for _, q := range []domain.QualityPolicy{domain.QualityFast, domain.QualityBalanced, domain.QualityMaxQuality} {
		r, ok := c.Policies[q]
		if !ok {
			errs = append(errs, fmt.Errorf("policies: no route for %s", q))
			continue
		}
		if r.Takes <= 0 {
			errs = append(errs, fmt.Errorf("policies %s: takes must be positive", q))
		}
		errs = append(errs, c.checkVideoTarget("policies "+string(q)+" primary", r.Primary)...)
		errs = append(errs, c.checkVideoTarget("policies "+string(q)+" fallback", r.Fallback)...)
	}
	return errs
}

func (c Config) validateRouting() []error {
	var errs []error
	for _, q := range slices.Sorted(maps.Keys(c.Routing.Weights)) {
		if !q.Valid() {
			errs = append(errs, fmt.Errorf("routing weights: %q is not a Quality Policy", q))
		}
	}
	for _, q := range slices.Sorted(maps.Keys(c.Routing.Candidates)) {
		if !q.Valid() {
			errs = append(errs, fmt.Errorf("routing candidates: %q is not a Quality Policy", q))
		}
	}
	for _, q := range domain.QualityPolicies() {
		for i, t := range c.Routing.Candidates[q] {
			errs = append(errs, c.checkVideoTarget(fmt.Sprintf("routing candidates %s[%d]", q, i), t)...)
		}
		w := c.Routing.Weights[q]
		if w.Quality < 0 || w.Cost < 0 || w.Fit < 0 || w.Quality+w.Cost+w.Fit <= 0 {
			errs = append(errs, fmt.Errorf("routing weights %s: must not be negative and must sum to more than 0", q))
		}
	}
	if r := c.Routing; r.MinSamples <= 0 || r.PriorQuality < 0 || r.PriorQuality > 1 || r.SwitchMargin < 0 {
		errs = append(errs, errors.New("routing: min_samples must be positive, prior_quality within 0-1 and switch_margin not negative"))
	}
	return errs
}

func (c Config) validateRetry() []error {
	if c.Retry.MaxAttempts <= 0 || c.Retry.BaseDelayMillis <= 0 || c.Retry.MaxDelayMillis < c.Retry.BaseDelayMillis ||
		c.Breaker.FailureThreshold <= 0 || c.Breaker.OpenSeconds <= 0 {
		return []error{errors.New("retry and breaker settings must be positive (max_delay_ms >= base_delay_ms)")}
	}
	return nil
}

// checkImageTarget checks one image model of a route; where names the route in the messages.
func (c Config) checkImageTarget(where string, t ImageTarget) []error {
	var errs []error
	mc, ok := c.Models[t.Model]
	if !slices.Contains(domain.ImageModels(), t.Model) || !ok || mc.Model == "" {
		errs = append(errs, fmt.Errorf("%s: %q is not a configured image model", where, t.Model))
	}
	if t.Fast && mc.FastModel == "" {
		errs = append(errs, fmt.Errorf("%s: %q has no fast_model", where, t.Model))
	}
	return append(errs, c.checkVendorSettings(where, t.Model)...)
}

// checkVideoTarget checks one video model at one resolution; where names the route in the messages.
func (c Config) checkVideoTarget(where string, t VideoTarget) []error {
	var errs []error
	caps, ok := Capabilities(t.Model)
	mc := c.Models[t.Model]
	switch {
	case !ok || mc.Model == "":
		errs = append(errs, fmt.Errorf("%s: %q is not a configured video model", where, t.Model))
	case !caps.SupportsResolution(t.Resolution):
		errs = append(errs, fmt.Errorf("%s: %q does not support %q", where, t.Model, t.Resolution))
	case mc.CostPerSecond[t.Resolution] <= 0:
		errs = append(errs, fmt.Errorf("%s: no cost_per_second_usd for %s at %s", where, t.Model, t.Resolution))
	}
	return append(errs, c.checkVendorSettings(where, t.Model)...)
}

func (c Config) checkVendorSettings(where string, m domain.ModelName) []error {
	if _, ok := c.Vendors[VendorOf(m)]; !ok {
		return []error{fmt.Errorf("%s: no vendor settings for %q", where, VendorOf(m))}
	}
	return nil
}
