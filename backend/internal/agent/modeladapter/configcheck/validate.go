// Package configcheck checks a modeladapter.Config: every route names a model that has an adapter
// and settings, and the numbers are sane. It sits beside modeladapter (which only reads the
// settings) so that the package, already over the line limit, does not grow.
package configcheck

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/chatgptid"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/gptimage"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Load is modeladapter.LoadConfig followed by Validate: the Config of the settings file, or an
// error that says what is wrong with it. An empty path selects the (valid) defaults. A file that
// still names Imagen 4 is read as Nano Banana (migrateLegacyModels).
func Load(path string) (modeladapter.Config, error) {
	cfg, err := modeladapter.LoadConfig(path)
	if err != nil {
		return modeladapter.Config{}, err
	}
	migrateLegacyModels(&cfg)
	cfg = withWorkflows(cfg)
	if err := Validate(cfg); err != nil {
		return modeladapter.Config{}, fmt.Errorf("model config %s: %w", path, err)
	}
	return cfg, nil
}

// Validate checks that every route names a model that has an adapter and settings, that video
// targets use a resolution the model supports and that the numbers are positive. Entries of a map
// are checked in key order, so the joined error lists the problems in the same order on every call.
func Validate(c modeladapter.Config) error {
	var errs []error
	for _, check := range []func(modeladapter.Config) []error{
		validateVendors, validateModels, validateLocal, validateImageRoutes,
		validatePolicies, validateRouting, validateRetry, func(c modeladapter.Config) []error { return c.Gentest.Validate() },
	} {
		errs = append(errs, check(c)...)
	}
	return errors.Join(errs...)
}

func validateVendors(c modeladapter.Config) []error {
	var errs []error
	for _, v := range slices.Sorted(maps.Keys(c.Vendors)) {
		vc := c.Vendors[v]
		if !slices.Contains(modeladapter.Vendors(), v) {
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
func validateModels(c modeladapter.Config) []error {
	var errs []error
	for _, m := range slices.Sorted(maps.Keys(c.Models)) {
		caps, video := modeladapter.Capabilities(m)
		if !slices.Contains(domain.ImageModels(), m) && !video && m != chatgptid.Model {
			errs = append(errs, fmt.Errorf("models: %q is not an image or video model", m))
		}
		if q := c.Models[m].Quality; !gptimage.Valid(q) || (q != "" && m != domain.ModelGPTImage2) {
			errs = append(errs, fmt.Errorf("models %s: quality %q is not one of %v (only %s has one)", m, q, gptimage.Qualities(), domain.ModelGPTImage2))
		}
		for _, res := range slices.Sorted(maps.Keys(c.Models[m].CostPerSecond)) {
			if video && !caps.SupportsResolution(res) {
				errs = append(errs, fmt.Errorf("models %s: cost_per_second_usd: %q is not a resolution of the model", m, res))
			}
		}
	}
	return errs
}

func validateImageRoutes(c modeladapter.Config) []error {
	var errs []error
	for _, p := range slices.Sorted(maps.Keys(c.Images)) {
		if !slices.Contains(modeladapter.ImagePurposes(), p) {
			errs = append(errs, fmt.Errorf("images: %q is not an image purpose", p))
		}
	}
	for _, p := range modeladapter.ImagePurposes() {
		r, ok := c.Images[p]
		if !ok {
			errs = append(errs, fmt.Errorf("images: no route for %s", p))
			continue
		}
		errs = append(errs, checkImageTarget(c, "images "+string(p)+" primary", r.Primary)...)
		errs = append(errs, checkImageTarget(c, "images "+string(p)+" fallback", r.Fallback)...)
	}
	return errs
}

func validatePolicies(c modeladapter.Config) []error {
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
		errs = append(errs, checkVideoTarget(c, "policies "+string(q)+" primary", r.Primary)...)
		errs = append(errs, checkVideoTarget(c, "policies "+string(q)+" fallback", r.Fallback)...)
	}
	return errs
}

func validateRouting(c modeladapter.Config) []error {
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
			errs = append(errs, checkVideoTarget(c, fmt.Sprintf("routing candidates %s[%d]", q, i), t)...)
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

func validateRetry(c modeladapter.Config) []error {
	if c.Retry.MaxAttempts <= 0 || c.Retry.BaseDelayMillis <= 0 || c.Retry.MaxDelayMillis < c.Retry.BaseDelayMillis ||
		c.Breaker.FailureThreshold <= 0 || c.Breaker.OpenSeconds <= 0 {
		return []error{errors.New("retry and breaker settings must be positive (max_delay_ms >= base_delay_ms)")}
	}
	return nil
}

// checkImageTarget checks one image model of a route; where names the route in the messages.
func checkImageTarget(c modeladapter.Config, where string, t modeladapter.ImageTarget) []error {
	var errs []error
	fast, configured := "", false
	if lm, _, local := c.LocalModel(t.Model); local {
		configured = lm.Media == localcfg.MediaImage
	} else {
		mc, ok := c.Models[t.Model]
		fast, configured = mc.FastModel, slices.Contains(domain.ImageModels(), t.Model) && ok && mc.Model != ""
		// Valid while image generation by subscription is off, too: a route that names it then fails over.
		configured = configured || t.Model == chatgptid.Model
	}
	if !configured {
		errs = append(errs, fmt.Errorf("%s: %q is not a configured image model", where, t.Model))
	}
	if t.Fast && fast == "" {
		errs = append(errs, fmt.Errorf("%s: %q has no fast_model", where, t.Model))
	}
	return append(errs, checkVendorSettings(c, where, t.Model)...)
}

// checkVideoTarget checks one video model at one resolution; where names the route in the messages.
func checkVideoTarget(c modeladapter.Config, where string, t modeladapter.VideoTarget) []error {
	var errs []error
	caps, ok := c.VideoCaps(t.Model)
	mc := c.Models[t.Model]
	switch {
	case !ok || (mc.Model == "" && !isLocal(c, t.Model)):
		errs = append(errs, fmt.Errorf("%s: %q is not a configured video model", where, t.Model))
	case !caps.SupportsResolution(t.Resolution):
		errs = append(errs, fmt.Errorf("%s: %q does not support %q", where, t.Model, t.Resolution))
	case mc.CostPerSecond[t.Resolution] <= 0 && !isLocal(c, t.Model): // a local model's price may be 0
		errs = append(errs, fmt.Errorf("%s: no cost_per_second_usd for %s at %s", where, t.Model, t.Resolution))
	}
	return append(errs, checkVendorSettings(c, where, t.Model)...)
}

func checkVendorSettings(c modeladapter.Config, where string, m domain.ModelName) []error {
	if _, saved := c.Workflows[string(m)]; saved {
		return nil // a saved workflow runs on the endpoint it names, which may not be registered yet: it has no backend until it is
	}
	if _, ok := c.VendorSettings()[modeladapter.VendorOf(m)]; !ok {
		return []error{fmt.Errorf("%s: no vendor settings for %q", where, modeladapter.VendorOf(m))}
	}
	return nil
}
