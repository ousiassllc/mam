package modeladapter

import (
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/chatgptid"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/videocaps"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// VideoCapabilities is what a video model can do. The Generation Orchestrator picks models and
// inputs from it (spec/spec.md §11: "モデル能力"). The data of the built-in models is package videocaps.
type VideoCapabilities videocaps.Capabilities

// Capabilities returns the capabilities of a video model, a registered one (PublishLocal) included.
func Capabilities(m domain.ModelName) (VideoCapabilities, bool) {
	if c, ok := videocaps.Of(m); ok {
		return VideoCapabilities(c), true
	}
	return publishedCaps(m)
}

// SnapDuration returns the shortest supported length that holds the requested seconds, or the
// longest supported length when the request is longer than any of them (the Editor trims later).
func (c VideoCapabilities) SnapDuration(seconds float64) float64 {
	if i := slices.IndexFunc(c.Durations, func(d float64) bool { return d >= seconds-1e-9 }); i >= 0 {
		return c.Durations[i]
	}
	return c.Durations[len(c.Durations)-1]
}

// SupportsResolution reports whether the model can produce the resolution.
func (c VideoCapabilities) SupportsResolution(r string) bool {
	return slices.Contains(c.Resolutions, r)
}

// AcceptsImageInputs reports whether an image model uses reference images.
func AcceptsImageInputs(m domain.ModelName) bool {
	return m == domain.ModelNanoBanana || m == domain.ModelNanoBananaLite || m == domain.ModelFlux2 || VendorOf(m) == VendorOpenAI || chatgptid.Is(m)
}

// SupportsSeed reports whether the vendor of a built-in model takes a seed (OpenAI, Kling, Seedance 2.0 have no such field).
func SupportsSeed(m domain.ModelName) bool {
	return m == domain.ModelFlux2 || m == domain.ModelRunwayGen45
}
