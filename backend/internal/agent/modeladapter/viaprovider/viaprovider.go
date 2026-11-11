// Package viaprovider is the table of how the API aggregators (OpenRouter, fal.ai, Replicate) serve the cloud video models that
// have one more way than the maker's own API (spec/architecture/model-adapters.md §9.6): which provider serves which model,
// the endpoint to call, the price of a second of video, what the model can do there, and why a provider that does not serve
// a model does not. It is data only (no HTTP): modeladapter reads it for the capabilities and the estimates, and package
// providerbackends calls the endpoints it names.
//
// What each aggregator serves of Kling was read from its public model pages on 2026-11-10. The prices are those of silent
// video (mam asks for no sound, like the official Kling 3.0 row of modeladapter's defaults) and reference values like every
// price of the Model Adapter:
//
//	              Kling 3.0 (720p = standard / 1080p = pro)    Kling 4.0 (720p / 1080p)
//	OpenRouter    kwaivgi/kling-v3.0-std / -pro  0.084 / 0.112  not offered (v3.0 Pro / Standard and O1 only)
//	fal.ai        fal-ai/kling-video/v3/standard / pro  0.084 / 0.112   kling/v4  0.157 / 0.235
//	Replicate     kwaivgi/kling-v3-video  0.168 / 0.224   kwaivgi/kling-v4.0  0.157 / 0.235
package viaprovider

import (
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/videocaps"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Provider is the API provider the cloud video models of Models are called through: the maker's own API (Official), or an API
// aggregator that resells the model by usage. One setting for all of them (modeladapter.Config.VideoProvider).
type Provider string

// Providers. The value is what models.json (video_provider), the settings screen and the MCP tools carry.
const (
	Official   Provider = "official"
	OpenRouter Provider = "openrouter"
	Fal        Provider = "fal"
	Replicate  Provider = "replicate"
)

// The tables of this package, built once; callers read what they return and must not modify it.
var (
	providers = []Provider{Official, OpenRouter, Fal, Replicate}
	models    = []domain.ModelName{domain.ModelKling3, domain.ModelKling4}
	routes    = buildRoutes() // by provider other than Official, then model
)

// Providers lists the providers in the order the settings screen shows them.
func Providers() []Provider { return providers }

// Valid reports whether p is one of Providers.
func (p Provider) Valid() bool { return slices.Contains(Providers(), p) }

// Selected is p with the default applied: the setting that is empty is Official.
func (p Provider) Selected() Provider {
	if p == "" {
		return Official
	}
	return p
}

// Label is the name of the provider on the screen.
func (p Provider) Label() string {
	switch p.Selected() {
	case Official:
		return "公式（各社の API）"
	case OpenRouter:
		return "OpenRouter"
	case Fal:
		return "fal.ai"
	case Replicate:
		return "Replicate"
	}
	return string(p)
}

// Vendor is the key of the API key the provider's calls need (the credential.Store key, which is also the Model Adapter's vendor of
// the provider); empty for Official, whose key is that of each model's own vendor.
func (p Provider) Vendor() string {
	switch p {
	case OpenRouter, Fal, Replicate:
		return string(p)
	}
	return ""
}

// Models lists the video models a provider other than Official can be asked to serve, in the order the screen lists them.
func Models() []domain.ModelName { return models }

// ModelLabel is the name of a model of Models on the screen.
func ModelLabel(m domain.ModelName) string {
	switch m {
	case domain.ModelKling3:
		return "Kling 3.0"
	case domain.ModelKling4:
		return "Kling 4.0"
	}
	return string(m)
}

// Variant is one way a provider serves a model at one resolution: where the job goes, the value of its tier or resolution
// parameter, and the price of a second of video.
type Variant struct {
	// Endpoint is OpenRouter's model slug, fal.ai's endpoint path without the final "/text-to-video" or "/image-to-video", or
	// Replicate's "owner/name" of the model.
	Endpoint string
	// Setting is the value of the parameter that chooses the tier or the resolution at the provider ("" when the Endpoint alone does).
	Setting string
	// USDPerSecond is the provider's price of a second of silent video.
	USDPerSecond float64
}

// Route is how one provider serves one video model. Why is set, and the rest empty, when it does not.
type Route struct {
	Provider Provider
	Model    domain.ModelName
	// Why is the sentence for the user that says why the provider does not serve the model and where it can be used; "" when it does.
	Why string
	// Variants are the ways to serve the model, by mam resolution (the resolutions of Caps).
	Variants map[string]Variant
	// Caps are what the model can do through this provider: they replace the official ones while the provider is selected. Like the route, they are shared by
	// all callers (Config.VideoCaps returns them as they are, the Durations and Resolutions slices included, as it does for a local model): read them, do not modify.
	Caps videocaps.Capabilities
	// FoldsNegative says the provider has no field for the negative prompt: its backend writes it into the prompt ("Avoid: ..."), so Prepare must count it in the length of the prompt.
	FoldsNegative bool
}

// Supported reports whether the provider serves the model.
func (r Route) Supported() bool { return r.Why == "" }

// Routes lists the route of every model of Models through p (none for Official, whose routes are the models' own).
func Routes(p Provider) []Route {
	var out []Route
	for _, m := range Models() {
		if r, ok := RouteOf(p, m); ok {
			out = append(out, r)
		}
	}
	return out
}

// RouteOf is the route of m through p. It is false when the provider setting does not concern the model: p is Official (or unset)
// or m is not one of Models. The route is shared by all callers (its Variants map included): do not modify it.
func RouteOf(p Provider, m domain.ModelName) (Route, bool) {
	r, ok := routes[p][m]
	return r, ok
}

func buildRoutes() map[Provider]map[domain.ModelName]Route {
	build := map[Provider]func(domain.ModelName) Route{OpenRouter: openRouter, Fal: fal, Replicate: replicate}
	out := make(map[Provider]map[domain.ModelName]Route, len(build))
	for p, fn := range build {
		out[p] = make(map[domain.ModelName]Route, len(models))
		for _, m := range models {
			out[p][m] = fn(m)
		}
	}
	return out
}

// FoldsNegative is whether the backend of p writes the negative prompt of m into the prompt (Route.FoldsNegative); false when the setting does not concern the model.
func FoldsNegative(p Provider, m domain.ModelName) bool {
	r, _ := RouteOf(p, m)
	return r.FoldsNegative
}

// Refusal is why p does not serve m (see Route.Why); "" when it does and when the setting does not concern the model.
func Refusal(p Provider, m domain.ModelName) string {
	r, _ := RouteOf(p, m)
	return r.Why
}

// KeyLabel is the name of the provider whose API key m needs when p sends it to an aggregator ("OpenRouter"); "" when the model's own
// vendor serves it (p is Official, the model is not one of Models, or p does not serve it).
func KeyLabel(p Provider, m domain.ModelName) string {
	if r, ok := RouteOf(p, m); ok && r.Supported() {
		return p.Label()
	}
	return ""
}

// PerSecond is the price of a second of m at resolution through p; false when p does not serve it (the price of the model's
// own vendor applies, or none).
func PerSecond(p Provider, m domain.ModelName, resolution string) (float64, bool) {
	r, ok := RouteOf(p, m)
	if !ok || !r.Supported() {
		return 0, false
	}
	v, ok := r.Variants[resolution]
	return v.USDPerSecond, ok
}

// kling3Caps are the capabilities of Kling 3.0 through an aggregator: 3-15 s in whole seconds (the official API of the model
// is asked for 5 or 10 s only), the two tiers, a first and a last frame. Free reference images and elements are not used.
func kling3Caps() videocaps.Capabilities {
	return videocaps.Capabilities{Durations: videocaps.WholeSeconds(3, 15), Resolutions: []string{"720p", "1080p"}, FirstFrame: true, LastFrame: true}
}

// kling4Caps are those of Kling 4.0 through an aggregator, the official ones: 3-30 s, 720p and 1080p, first and last frame.
func kling4Caps() videocaps.Capabilities {
	return videocaps.Capabilities{Durations: videocaps.WholeSeconds(3, 30), Resolutions: []string{"720p", "1080p"}, FirstFrame: true, LastFrame: true}
}

func route(p Provider, m domain.ModelName, v map[string]Variant) Route {
	caps := kling4Caps()
	if m == domain.ModelKling3 {
		caps = kling3Caps()
	}
	return Route{Provider: p, Model: m, Variants: v, Caps: caps}
}

func refused(p Provider, m domain.ModelName, why string) Route {
	return Route{Provider: p, Model: m, Why: why}
}

func openRouter(m domain.ModelName) Route {
	if m == domain.ModelKling4 {
		return refused(OpenRouter, m, "OpenRouter は Kling 4.0 を提供していません（Kling は v3.0 Pro / Standard と O1 のみ）。fal.ai・Replicate・公式のどれかを選ぶと使えます")
	}
	r := route(OpenRouter, m, map[string]Variant{
		"720p":  {Endpoint: "kwaivgi/kling-v3.0-std", USDPerSecond: 0.084},
		"1080p": {Endpoint: "kwaivgi/kling-v3.0-pro", USDPerSecond: 0.112},
	})
	r.FoldsNegative = true
	return r
}

func fal(m domain.ModelName) Route {
	if m == domain.ModelKling3 {
		return route(Fal, m, map[string]Variant{
			"720p":  {Endpoint: "fal-ai/kling-video/v3/standard", USDPerSecond: 0.084},
			"1080p": {Endpoint: "fal-ai/kling-video/v3/pro", USDPerSecond: 0.112},
		})
	}
	return route(Fal, m, map[string]Variant{
		"720p":  {Endpoint: "kling/v4", Setting: "720p", USDPerSecond: 0.157},
		"1080p": {Endpoint: "kling/v4", Setting: "1080p", USDPerSecond: 0.235},
	})
}

func replicate(m domain.ModelName) Route {
	if m == domain.ModelKling3 {
		return route(Replicate, m, map[string]Variant{
			"720p":  {Endpoint: "kwaivgi/kling-v3-video", Setting: "standard", USDPerSecond: 0.168},
			"1080p": {Endpoint: "kwaivgi/kling-v3-video", Setting: "pro", USDPerSecond: 0.224},
		})
	}
	return route(Replicate, m, map[string]Variant{
		"720p":  {Endpoint: "kwaivgi/kling-v4.0", Setting: "720p", USDPerSecond: 0.157},
		"1080p": {Endpoint: "kwaivgi/kling-v4.0", Setting: "1080p", USDPerSecond: 0.235},
	})
}
