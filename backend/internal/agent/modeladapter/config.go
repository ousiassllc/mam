package modeladapter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/chatgptid"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/gentestcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/overlay"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// ImagePurpose selects the image model (spec/architecture/model-adapters.md §3, one row each).
type ImagePurpose string

// Image purposes.
const (
	// PurposeNewReference: new Character / Product / Location references (GPT Image 2, then Nano Banana).
	PurposeNewReference ImagePurpose = "new_reference"
	// PurposeEditReference: Style / Lighting references, Keyframes and edits of existing material
	// (Nano Banana, then GPT Image 2).
	PurposeEditReference ImagePurpose = "edit_reference"
	// PurposePreview: fast, cheap iterations (FLUX.2, then Nano Banana Lite).
	PurposePreview ImagePurpose = "preview"
	// PurposeUnifiedOpenAI: one vendor for reasoning and images (GPT Image 2, then Nano Banana).
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
// (spec/architecture/model-adapters-m3.md §13.3).
type RoutingWeights struct {
	Quality float64 `json:"quality"`
	Cost    float64 `json:"cost"`
	Fit     float64 `json:"fit"`
}

// RoutingConfig is the setting of Multi-model routing (model-adapters-m3.md §13).
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
	// CostPerImage is the estimated cost of one image (USD), for image models. GPT Image 2 without one is priced by Quality.
	CostPerImage float64 `json:"cost_per_image_usd,omitempty"`
	Quality      string  `json:"quality,omitempty"` // the quality profile sent to GPT Image 2 (gptimage.Qualities; empty: gptimage.Default)
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
	// Endpoints are the servers the user registered (ComfyUI, OpenAI-compatible) and LocalModels the models
	// defined on them, keyed "<endpoint>/<name>"; a model is routed to as localcfg.ID (spec/architecture/local-models.md).
	Endpoints   map[string]localcfg.Endpoint      `json:"endpoints,omitempty"`
	LocalModels map[string]localcfg.Model         `json:"local_models,omitempty"`
	Workflows   map[string]localcfg.SavedWorkflow `json:"-"` // saved in mam, by identifier; configcheck.Load attaches them
	// PreferOpenAI routes new references through PurposeUnifiedOpenAI (§3, row "OpenAI系 LLM と認証・請求を統合したい場合").
	PreferOpenAI bool          `json:"prefer_openai"`
	Retry        RetryConfig   `json:"retry"`
	Breaker      BreakerConfig `json:"breaker"`
	// Routing is Multi-model routing on top of Policies (model-adapters-m3.md §13).
	Routing RoutingConfig     `json:"routing"`
	Gentest gentestcfg.Limits `json:"gentest"` // the limits of one batch of the generation test screen
}

// DefaultConfig is the assignment of model-adapters.md §3, §4 for M1. The prices are reference values.
func DefaultConfig() Config {
	return Config{
		Vendors: map[Vendor]VendorConfig{
			VendorGoogle:   {BaseURL: "https://generativelanguage.googleapis.com", MaxConcurrency: 4, TimeoutSeconds: 600},
			VendorOpenAI:   {BaseURL: "https://api.openai.com", MaxConcurrency: 4, TimeoutSeconds: 180},
			VendorFlux:     {BaseURL: "https://api.bfl.ai", MaxConcurrency: 4, TimeoutSeconds: 180},
			VendorKling:    {BaseURL: "https://api.klingai.com", MaxConcurrency: 3, TimeoutSeconds: 900},
			VendorKling4:   {BaseURL: "https://api-singapore.klingai.com", MaxConcurrency: 3, TimeoutSeconds: 900},
			VendorSeedance: {BaseURL: "https://ark.ap-southeast.bytepluses.com", MaxConcurrency: 3, TimeoutSeconds: 900},
			VendorRunway:   {BaseURL: "https://api.dev.runwayml.com", MaxConcurrency: 2, TimeoutSeconds: 900},
			VendorMiniMax:  {BaseURL: "https://api.minimax.io", MaxConcurrency: 3, TimeoutSeconds: 900},
			VendorAlibaba:  {BaseURL: alibabaBaseURL, MaxConcurrency: 3, TimeoutSeconds: 1800},
			VendorLuma:     {BaseURL: "https://agents.lumalabs.ai", MaxConcurrency: 3, TimeoutSeconds: 900},
		},
		Models: defaultModels(),
		Images: map[ImagePurpose]ImageRoute{
			PurposeNewReference:  {Primary: ImageTarget{Model: domain.ModelGPTImage2}, Fallback: ImageTarget{Model: domain.ModelNanoBanana}},
			PurposeEditReference: {Primary: ImageTarget{Model: domain.ModelNanoBanana}, Fallback: ImageTarget{Model: domain.ModelGPTImage2}},
			PurposePreview:       {Primary: ImageTarget{Model: domain.ModelFlux2}, Fallback: ImageTarget{Model: domain.ModelNanoBananaLite}},
			PurposeUnifiedOpenAI: {Primary: ImageTarget{Model: domain.ModelGPTImage2}, Fallback: ImageTarget{Model: domain.ModelNanoBanana}},
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
		// model-adapters-m3.md §13.2, §13.3: FAST weighs cost most, MAX QUALITY the Judge record.
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
// The result is not checked: configcheck.Load does that.
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
		overlay.Entries(cfg.Vendors, file.Vendors), overlay.Entries(cfg.Models, file.Models),
		overlay.Entries(cfg.Images, file.Images), overlay.Entries(cfg.Policies, file.Policies),
		overlay.Entries(cfg.Routing.Weights, file.Routing.Weights),
	} {
		if err != nil {
			return Config{}, fmt.Errorf("decode model config %s: %w", path, err)
		}
	}
	return cfg, nil
}

// VendorSettings are the built-in vendors and the registered endpoints (vendors named by localcfg.VendorKey).
func (c Config) VendorSettings() map[Vendor]VendorConfig {
	out := maps.Clone(c.Vendors)
	if out == nil {
		out = map[Vendor]VendorConfig{}
	}
	out[Vendor(chatgptid.Vendor)] = VendorConfig{BaseURL: chatgptid.BaseURL, MaxConcurrency: 1, TimeoutSeconds: chatgptid.TimeoutSeconds}
	for name, e := range c.Endpoints {
		out[Vendor(localcfg.VendorKey(name))] = VendorConfig{BaseURL: e.BaseURL, MaxConcurrency: e.Concurrency(), TimeoutSeconds: e.Timeout()}
	}
	return out
}

// LocalModel returns the model defined on a registered endpoint that the identifier names.
func (c Config) LocalModel(m domain.ModelName) (localcfg.Model, localcfg.Kind, bool) {
	if w, ok := c.Workflows[string(m)]; ok {
		return w.Model, localcfg.KindComfyUI, true
	}
	kind, endpoint, name, ok := localcfg.ParseID(string(m))
	if !ok || c.Endpoints[endpoint].Kind != kind {
		return localcfg.Model{}, "", false
	}
	lm, ok := c.LocalModels[localcfg.Key(endpoint, name)]
	return lm, kind, ok
}

// ModelSettings are the settings of a model: its entry of Models, or those a local model stands for.
func (c Config) ModelSettings(m domain.ModelName) ModelConfig {
	if lm, _, ok := c.LocalModel(m); ok {
		return ModelConfig{Model: lm.Model, CostPerImage: lm.CostPerImage}
	}
	return c.Models[m]
}

// VideoCaps are the capabilities of a video model, built-in or defined on an endpoint.
func (c Config) VideoCaps(m domain.ModelName) (VideoCapabilities, bool) {
	if lm, _, ok := c.LocalModel(m); ok {
		caps, isVideo := lm.VideoCaps()
		return VideoCapabilities(caps), isVideo
	}
	return Capabilities(m)
}

// negativeModel is m when it is a local model whose workflow takes a negative prompt, else "".
func (c Config) negativeModel(m domain.ModelName) domain.ModelName {
	if lm, _, ok := c.LocalModel(m); ok && lm.Workflow != nil && lm.Workflow.Has(localcfg.ParamNegativePrompt) {
		return m
	}
	return ""
}

// AcceptsImages reports whether an image model uses reference images.
func (c Config) AcceptsImages(m domain.ModelName) bool {
	if lm, kind, ok := c.LocalModel(m); ok {
		return lm.ImageInputs(kind)
	}
	return AcceptsImageInputs(m)
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
	if lm, _, ok := c.LocalModel(t.Model); ok {
		return lm.CostPerSecond * seconds
	}
	if t.Model == domain.ModelLumaRay32 {
		return lumaCost(c.Models[t.Model].CostPerSecond[t.Resolution], seconds)
	}
	return c.Models[t.Model].CostPerSecond[t.Resolution] * seconds
}
