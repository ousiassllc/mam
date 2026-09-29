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
		},
		Models: map[domain.ModelName]ModelConfig{
			domain.ModelImagen4:    {Model: "imagen-4.0-generate-001", FastModel: "imagen-4.0-fast-generate-001", CostPerImage: 0.04},
			domain.ModelNanoBanana: {Model: "gemini-2.5-flash-image", CostPerImage: 0.04},
			domain.ModelFlux2:      {Model: "flux-2-pro", CostPerImage: 0.03},
			domain.ModelGPTImage2:  {Model: "gpt-image-1", CostPerImage: 0.06},
			domain.ModelKling3:     {Model: "kling-v3", CostPerSecond: map[string]float64{"720p": 0.07, "1080p": 0.14}},
			domain.ModelSeedance2:  {Model: "seedance-2-0", CostPerSecond: map[string]float64{"480p": 0.03, "720p": 0.08, "1080p": 0.18}},
		},
		Images: map[ImagePurpose]ImageRoute{
			PurposeNewReference:  {Primary: ImageTarget{Model: domain.ModelImagen4}, Fallback: ImageTarget{Model: domain.ModelGPTImage2}},
			PurposeEditReference: {Primary: ImageTarget{Model: domain.ModelNanoBanana}, Fallback: ImageTarget{Model: domain.ModelImagen4}},
			PurposePreview:       {Primary: ImageTarget{Model: domain.ModelFlux2}, Fallback: ImageTarget{Model: domain.ModelImagen4, Fast: true}},
			PurposeUnifiedOpenAI: {Primary: ImageTarget{Model: domain.ModelGPTImage2}, Fallback: ImageTarget{Model: domain.ModelImagen4}},
		},
		// model-adapters.md §4. MAX QUALITY names Runway Gen-4.5, which is added in M2; until then
		// Seedance 2.0 at 1080p is the primary and Kling 3.0 at 1080p the alternative.
		Policies: map[domain.QualityPolicy]PolicyRoute{
			domain.QualityFast:       {Primary: VideoTarget{domain.ModelKling3, "720p"}, Fallback: VideoTarget{domain.ModelSeedance2, "480p"}, Takes: 2},
			domain.QualityBalanced:   {Primary: VideoTarget{domain.ModelSeedance2, "720p"}, Fallback: VideoTarget{domain.ModelKling3, "720p"}, Takes: 3},
			domain.QualityMaxQuality: {Primary: VideoTarget{domain.ModelSeedance2, "1080p"}, Fallback: VideoTarget{domain.ModelKling3, "1080p"}, Takes: 4},
		},
		Retry:   RetryConfig{MaxAttempts: 3, BaseDelayMillis: 1000, MaxDelayMillis: 16000},
		Breaker: BreakerConfig{FailureThreshold: 5, OpenSeconds: 30},
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
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode model config %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("model config %s: %w", path, err)
	}
	return cfg, nil
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
