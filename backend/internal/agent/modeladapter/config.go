package modeladapter

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// ImagePurpose selects the image model (spec/architecture/model-adapters.md §3, one row each).
type ImagePurpose string

// Image purposes.
const (
	// PurposeNewReference: new Character / Product / Location references (Imagen 4, then GPT Image 2).
	PurposeNewReference ImagePurpose = "new_reference"
	// PurposeEditReference: Style / Lighting references, Keyframes and edits of existing material
	// (Nano Banana, then Imagen 4).
	PurposeEditReference ImagePurpose = "edit_reference"
	// PurposePreview: fast, cheap iterations (FLUX.2, then Imagen 4 Fast).
	PurposePreview ImagePurpose = "preview"
	// PurposeUnifiedOpenAI: one vendor for reasoning and images (GPT Image 2, then Imagen 4).
	PurposeUnifiedOpenAI ImagePurpose = "unified_openai"
)

// ImagePurposes lists every purpose.
func ImagePurposes() []ImagePurpose {
	return []ImagePurpose{PurposeNewReference, PurposeEditReference, PurposePreview, PurposeUnifiedOpenAI}
}

// ImageTarget is one image model of a route. Fast selects the model's fast variant.
type ImageTarget struct {
	Model domain.ModelName `json:"model"`
	Fast  bool             `json:"fast,omitempty"`
}

// ImageRoute is the primary and the alternative model of an image purpose.
type ImageRoute struct {
	Primary  ImageTarget `json:"primary"`
	Fallback ImageTarget `json:"fallback"`
}

// VideoTarget is one video model at one resolution.
type VideoTarget struct {
	Model      domain.ModelName `json:"model"`
	Resolution string           `json:"resolution"`
}

// PolicyRoute is the video generation of one Quality Policy (F-22): the models to use and how many Takes.
type PolicyRoute struct {
	Primary  VideoTarget `json:"primary"`
	Fallback VideoTarget `json:"fallback"`
	Takes    int         `json:"takes"`
}

// RoutingWeights are the weights of the three values Multi-model routing scores a video model by
// (spec/architecture/model-adapters.md §13.3).
type RoutingWeights struct {
	Quality float64 `json:"quality"`
	Cost    float64 `json:"cost"`
	Fit     float64 `json:"fit"`
}

// RoutingConfig is the setting of Multi-model routing (model-adapters.md §13).
type RoutingConfig struct {
	// Enabled switches it on; off means the Quality Policy's assignment alone (M1).
	Enabled bool `json:"enabled"`
	// MinSamples is the number of judged Takes a model needs before its record counts.
	MinSamples int `json:"min_samples"`
	// PriorQuality (0-1) is the quality a model without a record is assumed to have.
	PriorQuality float64 `json:"prior_quality"`
	// SwitchMargin is how much better a challenger must score than the Quality Policy's primary.
	SwitchMargin float64                                 `json:"switch_margin"`
	Weights      map[domain.QualityPolicy]RoutingWeights `json:"weights"`
	// Candidates are the video models (with a resolution) weighed for each Quality Policy.
	Candidates map[domain.QualityPolicy][]VideoTarget `json:"candidates"`
}

// VendorConfig holds the connection settings of a vendor.
type VendorConfig struct {
	// BaseURL is the API origin.
	BaseURL string `json:"base_url"`
	// MaxConcurrency is the most calls in flight at once. It is lowered when the vendor answers
	// 429 and raised again step by step while calls succeed.
	MaxConcurrency int `json:"max_concurrency"`
	// TimeoutSeconds bounds one call (submit, wait and download for a video).
	TimeoutSeconds int `json:"timeout_seconds"`
}

// ModelConfig holds the settings of one model. The prices are reference values (spec/architecture/
// model-adapters.md §8): they feed the estimated cost, not billing.
type ModelConfig struct {
	// Model is the vendor's identifier of the model. FastModel is used for ImageTarget.Fast.
	Model     string `json:"model"`
	FastModel string `json:"fast_model,omitempty"`
	// CostPerImage is the estimated cost of one image (USD), for image models.
	CostPerImage float64 `json:"cost_per_image_usd,omitempty"`
	// CostPerSecond is the estimated cost of one second of video (USD) by resolution.
	CostPerSecond map[string]float64 `json:"cost_per_second_usd,omitempty"`
}

// RetryConfig is the exponential backoff: a failed attempt is repeated after BaseDelay,
// 2*BaseDelay ... (at most MaxDelay), MaxAttempts calls in total.
type RetryConfig struct {
	MaxAttempts     int `json:"max_attempts"`
	BaseDelayMillis int `json:"base_delay_ms"`
	MaxDelayMillis  int `json:"max_delay_ms"`
}

// BreakerConfig is the per-vendor circuit breaker.
type BreakerConfig struct {
	FailureThreshold int `json:"failure_threshold"`
	OpenSeconds      int `json:"open_seconds"`
}

// Config is the Model Adapter setting. DefaultConfig holds the assignment of
// spec/architecture/model-adapters.md §3, §4; a JSON file (LoadConfig) overrides any part of it.
type Config struct {
	Vendors  map[Vendor]VendorConfig              `json:"vendors"`
	Models   map[domain.ModelName]ModelConfig     `json:"models"`
	Images   map[ImagePurpose]ImageRoute          `json:"images"`
	Policies map[domain.QualityPolicy]PolicyRoute `json:"policies"`
	// PreferOpenAI routes new references through PurposeUnifiedOpenAI (§3, row "OpenAI系 LLM と認証・請求を統合したい場合").
	PreferOpenAI bool          `json:"prefer_openai"`
	Retry        RetryConfig   `json:"retry"`
	Breaker      BreakerConfig `json:"breaker"`
	// Routing is Multi-model routing on top of Policies (model-adapters.md §13).
	Routing RoutingConfig `json:"routing"`
}

// DefaultConfig is the assignment of model-adapters.md §3, §4 for M1. The prices are reference values.
func DefaultConfig() Config {
	return Config{
		Vendors: map[Vendor]VendorConfig{
			VendorGoogle:   {BaseURL: "https://generativelanguage.googleapis.com", MaxConcurrency: 4, TimeoutSeconds: 120},
			VendorOpenAI:   {BaseURL: "https://api.openai.com", MaxConcurrency: 4, TimeoutSeconds: 180},
			VendorFlux:     {BaseURL: "https://api.bfl.ai", MaxConcurrency: 4, TimeoutSeconds: 180},
			VendorKling:    {BaseURL: "https://api.klingai.com", MaxConcurrency: 3, TimeoutSeconds: 900},
			VendorSeedance: {BaseURL: "https://ark.ap-southeast.bytepluses.com", MaxConcurrency: 3, TimeoutSeconds: 900},
			VendorRunway:   {BaseURL: "https://api.dev.runwayml.com", MaxConcurrency: 2, TimeoutSeconds: 900},
		},
		Models: map[domain.ModelName]ModelConfig{
			domain.ModelImagen4:    {Model: "imagen-4.0-generate-001", FastModel: "imagen-4.0-fast-generate-001", CostPerImage: 0.04},
			domain.ModelNanoBanana: {Model: "gemini-2.5-flash-image", CostPerImage: 0.04},
			domain.ModelFlux2:      {Model: "flux-2-pro", CostPerImage: 0.03},
			domain.ModelGPTImage2:  {Model: "gpt-image-1", CostPerImage: 0.06},
			domain.ModelKling3:     {Model: "kling-v3", CostPerSecond: map[string]float64{"720p": 0.07, "1080p": 0.14}},
			domain.ModelSeedance2:  {Model: "seedance-2-0", CostPerSecond: map[string]float64{"480p": 0.03, "720p": 0.08, "1080p": 0.18}},
			// Runway prices in credits (12 credits per second of Gen-4.5, 1 credit = USD 0.01).
			domain.ModelRunwayGen45: {Model: "gen4.5", CostPerSecond: map[string]float64{"720p": 0.12}},
		},
		Images: map[ImagePurpose]ImageRoute{
			PurposeNewReference:  {Primary: ImageTarget{Model: domain.ModelImagen4}, Fallback: ImageTarget{Model: domain.ModelGPTImage2}},
			PurposeEditReference: {Primary: ImageTarget{Model: domain.ModelNanoBanana}, Fallback: ImageTarget{Model: domain.ModelImagen4}},
			PurposePreview:       {Primary: ImageTarget{Model: domain.ModelFlux2}, Fallback: ImageTarget{Model: domain.ModelImagen4, Fast: true}},
			PurposeUnifiedOpenAI: {Primary: ImageTarget{Model: domain.ModelGPTImage2}, Fallback: ImageTarget{Model: domain.ModelImagen4}},
		},
		// model-adapters.md §4. MAX QUALITY uses Runway Gen-4.5 (720p, its only output size); the
		// alternative ("Different Model") is Kling 3.0.
		Policies: map[domain.QualityPolicy]PolicyRoute{
			domain.QualityFast:       {Primary: VideoTarget{domain.ModelKling3, "720p"}, Fallback: VideoTarget{domain.ModelSeedance2, "480p"}, Takes: 2},
			domain.QualityBalanced:   {Primary: VideoTarget{domain.ModelSeedance2, "720p"}, Fallback: VideoTarget{domain.ModelKling3, "720p"}, Takes: 3},
			domain.QualityMaxQuality: {Primary: VideoTarget{domain.ModelRunwayGen45, "720p"}, Fallback: VideoTarget{domain.ModelKling3, "1080p"}, Takes: 4},
		},
		Retry:   RetryConfig{MaxAttempts: 3, BaseDelayMillis: 1000, MaxDelayMillis: 16000},
		Breaker: BreakerConfig{FailureThreshold: 5, OpenSeconds: 30},
		// model-adapters.md §13.2, §12.3: FAST weighs cost most, MAX QUALITY the Judge record.
		Routing: RoutingConfig{
			Enabled: true, MinSamples: 5, PriorQuality: 0.75, SwitchMargin: 0.05,
			Weights: map[domain.QualityPolicy]RoutingWeights{
				domain.QualityFast:       {Quality: 0.4, Cost: 0.5, Fit: 0.1},
				domain.QualityBalanced:   {Quality: 0.6, Cost: 0.3, Fit: 0.1},
				domain.QualityMaxQuality: {Quality: 0.85, Cost: 0.05, Fit: 0.1},
			},
			Candidates: map[domain.QualityPolicy][]VideoTarget{
				domain.QualityFast:       {{domain.ModelKling3, "720p"}, {domain.ModelSeedance2, "480p"}, {domain.ModelRunwayGen45, "720p"}},
				domain.QualityBalanced:   {{domain.ModelSeedance2, "720p"}, {domain.ModelKling3, "720p"}, {domain.ModelRunwayGen45, "720p"}},
				domain.QualityMaxQuality: {{domain.ModelRunwayGen45, "720p"}, {domain.ModelKling3, "1080p"}, {domain.ModelSeedance2, "1080p"}},
			},
		},
	}
}

// LoadConfig returns DefaultConfig with the settings of the JSON file applied on top. An empty
// path selects the defaults. Unknown keys are rejected so a typo does not silently change nothing.
func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path) // #nosec G304 -- the operator chooses the settings file
	if err != nil {
		return Config{}, fmt.Errorf("read model config: %w", err)
	}
	// The maps of entries are decoded per entry on top of the default entry, so that an entry that
	// names only some of its fields keeps the defaults of the others (encoding/json would rebuild the
	// whole entry from the zero value): vendors, models, images, policies and routing.weights. The
	// embedded Config shares the default maps; Routing shadows its Config field so that the weights
	// stay raw until they are overlaid.
	file := struct {
		Config
		Vendors  map[Vendor]json.RawMessage               `json:"vendors"`
		Models   map[domain.ModelName]json.RawMessage     `json:"models"`
		Images   map[ImagePurpose]json.RawMessage         `json:"images"`
		Policies map[domain.QualityPolicy]json.RawMessage `json:"policies"`
		Routing  struct {
			RoutingConfig
			Weights map[domain.QualityPolicy]json.RawMessage `json:"weights"`
		} `json:"routing"`
	}{Config: cfg}
	file.Routing.RoutingConfig = cfg.Routing
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&file); err != nil {
		return Config{}, fmt.Errorf("decode model config %s: %w", path, err)
	}
	cfg = file.Config
	cfg.Routing = file.Routing.RoutingConfig
	for _, err := range []error{
		overlayEntries(cfg.Vendors, file.Vendors), overlayEntries(cfg.Models, file.Models),
		overlayEntries(cfg.Images, file.Images), overlayEntries(cfg.Policies, file.Policies),
		overlayEntries(cfg.Routing.Weights, file.Routing.Weights),
	} {
		if err != nil {
			return Config{}, fmt.Errorf("decode model config %s: %w", path, err)
		}
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("model config %s: %w", path, err)
	}
	return cfg, nil
}

// overlayEntries applies the JSON of each entry on top of the entry of dst with the same key (the
// zero value for a new key); fields the JSON does not name keep their value. Unknown fields are rejected.
func overlayEntries[K comparable, V any](dst map[K]V, raw map[K]json.RawMessage) error {
	for k, r := range raw {
		v := dst[k]
		dec := json.NewDecoder(bytes.NewReader(r))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&v); err != nil {
			return fmt.Errorf("%v: %w", k, err)
		}
		dst[k] = v
	}
	return nil
}

// Validate checks that every route names a model that has an adapter and settings, that video
// targets use a resolution the model supports and that the numbers are positive.
func (c Config) Validate() error {
	var errs []error
	for v, vc := range c.Vendors {
		if !slices.Contains(Vendors(), v) {
			errs = append(errs, fmt.Errorf("vendor %q is unknown", v))
		}
		if vc.BaseURL == "" || vc.MaxConcurrency <= 0 || vc.TimeoutSeconds <= 0 {
			errs = append(errs, fmt.Errorf("vendor %s: base_url, max_concurrency and timeout_seconds must be set", v))
		}
	}
	// A typo in the key of a map entry would otherwise change nothing without a word.
	for m, mc := range c.Models {
		caps, video := Capabilities(m)
		if !slices.Contains(domain.ImageModels(), m) && !video {
			errs = append(errs, fmt.Errorf("models: %q is not an image or video model", m))
		}
		for res := range mc.CostPerSecond {
			if video && !caps.SupportsResolution(res) {
				errs = append(errs, fmt.Errorf("models %s: cost_per_second_usd: %q is not a resolution of the model", m, res))
			}
		}
	}
	for p := range c.Images {
		if !slices.Contains(ImagePurposes(), p) {
			errs = append(errs, fmt.Errorf("images: %q is not an image purpose", p))
		}
	}
	for q := range c.Policies {
		if !q.Valid() {
			errs = append(errs, fmt.Errorf("policies: %q is not a Quality Policy", q))
		}
	}
	for q := range c.Routing.Weights {
		if !q.Valid() {
			errs = append(errs, fmt.Errorf("routing weights: %q is not a Quality Policy", q))
		}
	}
	for q := range c.Routing.Candidates {
		if !q.Valid() {
			errs = append(errs, fmt.Errorf("routing candidates: %q is not a Quality Policy", q))
		}
	}
	image := func(where string, t ImageTarget) {
		mc, ok := c.Models[t.Model]
		if !slices.Contains(domain.ImageModels(), t.Model) || !ok || mc.Model == "" {
			errs = append(errs, fmt.Errorf("%s: %q is not a configured image model", where, t.Model))
		}
		if t.Fast && mc.FastModel == "" {
			errs = append(errs, fmt.Errorf("%s: %q has no fast_model", where, t.Model))
		}
		if _, ok := c.Vendors[VendorOf(t.Model)]; !ok {
			errs = append(errs, fmt.Errorf("%s: no vendor settings for %q", where, VendorOf(t.Model)))
		}
	}
	for _, p := range ImagePurposes() {
		r, ok := c.Images[p]
		if !ok {
			errs = append(errs, fmt.Errorf("images: no route for %s", p))
			continue
		}
		image("images "+string(p)+" primary", r.Primary)
		image("images "+string(p)+" fallback", r.Fallback)
	}
	video := func(where string, t VideoTarget) {
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
		if _, ok := c.Vendors[VendorOf(t.Model)]; !ok {
			errs = append(errs, fmt.Errorf("%s: no vendor settings for %q", where, VendorOf(t.Model)))
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
		video("policies "+string(q)+" primary", r.Primary)
		video("policies "+string(q)+" fallback", r.Fallback)
	}
	for _, q := range domain.QualityPolicies() {
		for i, t := range c.Routing.Candidates[q] {
			video(fmt.Sprintf("routing candidates %s[%d]", q, i), t)
		}
		w := c.Routing.Weights[q]
		if w.Quality < 0 || w.Cost < 0 || w.Fit < 0 || w.Quality+w.Cost+w.Fit <= 0 {
			errs = append(errs, fmt.Errorf("routing weights %s: must not be negative and must sum to more than 0", q))
		}
	}
	if r := c.Routing; r.MinSamples <= 0 || r.PriorQuality < 0 || r.PriorQuality > 1 || r.SwitchMargin < 0 {
		errs = append(errs, errors.New("routing: min_samples must be positive, prior_quality within 0-1 and switch_margin not negative"))
	}
	if c.Retry.MaxAttempts <= 0 || c.Retry.BaseDelayMillis <= 0 || c.Retry.MaxDelayMillis < c.Retry.BaseDelayMillis ||
		c.Breaker.FailureThreshold <= 0 || c.Breaker.OpenSeconds <= 0 {
		errs = append(errs, errors.New("retry and breaker settings must be positive (max_delay_ms >= base_delay_ms)"))
	}
	return errors.Join(errs...)
}

func (c Config) timeout(v Vendor) time.Duration {
	return time.Duration(c.Vendors[v].TimeoutSeconds) * time.Second
}

// ImageRouteFor returns the route of an image purpose, honouring PreferOpenAI for new references.
func (c Config) ImageRouteFor(p ImagePurpose) ImageRoute {
	if p == PurposeNewReference && c.PreferOpenAI {
		p = PurposeUnifiedOpenAI
	}
	return c.Images[p]
}

// VideoCost is the estimated cost of a clip of the given length.
func (c Config) VideoCost(t VideoTarget, seconds float64) float64 {
	return c.Models[t.Model].CostPerSecond[t.Resolution] * seconds
}

// ImageCost is the estimated cost of one image.
func (c Config) ImageCost(m domain.ModelName) float64 { return c.Models[m].CostPerImage }
