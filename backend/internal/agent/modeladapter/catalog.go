package modeladapter

import (
	"slices"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// VideoCapabilities is what a video model can do. The Generation Orchestrator picks models and
// inputs from it (spec/spec.md §11: "モデル能力").
type VideoCapabilities struct {
	// Durations are the clip lengths the model can produce, ascending (seconds).
	Durations []float64
	// Resolutions the model can produce.
	Resolutions []string
	// MaxReferenceImages is how many free reference images the model accepts. They cannot be combined
	// with the first and last frame in one request (Prepare keeps one of the two).
	MaxReferenceImages int
	FirstFrame         bool
	LastFrame          bool
	// NativeAudio means the model generates sound together with the picture.
	NativeAudio bool
}

// Capabilities returns the capabilities of a video model, a registered one (PublishLocal) included.
func Capabilities(m domain.ModelName) (VideoCapabilities, bool) {
	switch m {
	case domain.ModelKling3:
		// Image-to-video takes a first and a last frame; the text-to-video and image-to-video
		// endpoints have no free reference images.
		return VideoCapabilities{Durations: []float64{5, 10}, Resolutions: []string{"720p", "1080p"}, FirstFrame: true, LastFrame: true}, true
	case domain.ModelSeedance2:
		// Up to 12 reference files of all kinds: 3 are kept for the prompt, audio and video references.
		return VideoCapabilities{
			Durations: []float64{4, 5, 6, 7, 8, 9, 10, 11, 12}, Resolutions: []string{"480p", "720p", "1080p"},
			MaxReferenceImages: 9, FirstFrame: true, LastFrame: true, NativeAudio: true,
		}, true
	case domain.ModelRunwayGen45:
		// Text-to-video and image-to-video (first frame only) at 720p; no free reference images.
		return VideoCapabilities{Durations: []float64{2, 3, 4, 5, 6, 7, 8, 9, 10}, Resolutions: []string{"720p"}, FirstFrame: true}, true
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
	return m == domain.ModelNanoBanana || m == domain.ModelGPTImage2 || m == domain.ModelFlux2
}
