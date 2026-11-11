// Package videocaps holds what each built-in cloud video model can do (spec/spec.md §11 "モデル能力"): the data behind
// modeladapter.Capabilities, a package of its own because modeladapter is at its size limit. modeladapter.VideoCapabilities is
// defined as this package's Capabilities (a defined type with the same fields), so that one converts to the other.
package videocaps

import "github.com/ousiassllc/mam/backend/internal/domain"

// Capabilities is what a video model can do (modeladapter.VideoCapabilities).
type Capabilities struct {
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

// WholeSeconds lists the lengths from..to in whole seconds.
func WholeSeconds(from, to int) []float64 {
	out := make([]float64, 0, to-from+1)
	for s := from; s <= to; s++ {
		out = append(out, float64(s))
	}
	return out
}

// Of returns the capabilities of a built-in video model; false for any other model.
func Of(m domain.ModelName) (Capabilities, bool) {
	switch m {
	case domain.ModelKling3:
		// Image-to-video takes a first and a last frame; the text-to-video and image-to-video
		// endpoints have no free reference images.
		return Capabilities{Durations: []float64{5, 10}, Resolutions: []string{"720p", "1080p"}, FirstFrame: true, LastFrame: true}, true
	case domain.ModelSeedance2:
		// Up to 12 reference files of all kinds: 3 are kept for the prompt, audio and video references.
		return Capabilities{
			Durations: []float64{4, 5, 6, 7, 8, 9, 10, 11, 12}, Resolutions: []string{"480p", "720p", "1080p"},
			MaxReferenceImages: 9, FirstFrame: true, LastFrame: true, NativeAudio: true,
		}, true
	case domain.ModelSeedance25:
		// 4-30 s, up to 50 omni reference assets of which 30 are images (images are all mam sends). A first frame keeps the
		// ratio of the image; the draft mode (480p only), mov output, editing and extension are not used.
		return Capabilities{
			Durations: WholeSeconds(4, 30), Resolutions: []string{"480p", "720p", "1080p"},
			MaxReferenceImages: 30, FirstFrame: true, LastFrame: true, NativeAudio: true,
		}, true
	case domain.ModelRunwayGen45:
		// Text-to-video and image-to-video (first frame only) at 720p; no free reference images.
		return Capabilities{Durations: []float64{2, 3, 4, 5, 6, 7, 8, 9, 10}, Resolutions: []string{"720p"}, FirstFrame: true}, true
	case domain.ModelGeminiOmniFlash:
		// 3-10 s, 24 FPS, with native sound. 360p, 1080p and 4K exist but only the 720p price is published per second, so only
		// 720p is offered. First and last frame, or free reference images (the two are exclusive in Prepare); no length parameter:
		// the length is asked for in the prompt. The official documentation states no limit for reference images: 6 is its example.
		return Capabilities{
			Durations: []float64{3, 4, 5, 6, 7, 8, 9, 10}, Resolutions: []string{"720p"},
			MaxReferenceImages: 6, FirstFrame: true, LastFrame: true, NativeAudio: true,
		}, true
	case domain.ModelKling4:
		// 3-30 s in whole seconds, 720p and 1080p (the Capability Map also lists 4K, but the text-to-video and image-to-video
		// settings take 720p and 1080p). First and last frame; the omni reference endpoint is not used.
		return Capabilities{Durations: WholeSeconds(3, 30), Resolutions: []string{"720p", "1080p"}, FirstFrame: true, LastFrame: true}, true
	case domain.ModelMiniMaxH3:
		// 4-15 s in whole seconds, 768P and 2K (named as the API names them, lower case). First and last frame, or reference
		// images (the two modes are exclusive, which Prepare keeps): 5 reference images, the free quota of a task (the sixth
		// and beyond are billed apart); reference videos and audio are not used.
		return Capabilities{Durations: WholeSeconds(4, 15), Resolutions: []string{"768p", "2k"}, MaxReferenceImages: 5, FirstFrame: true, LastFrame: true}, true
	case domain.ModelMiniMaxH3Max:
		// 5-15 s, 480P and 768P. Same modes as H3; 2 reference images, the free quota of a task of H3 Max.
		return Capabilities{Durations: WholeSeconds(5, 15), Resolutions: []string{"480p", "768p"}, MaxReferenceImages: 2, FirstFrame: true, LastFrame: true}, true
	case domain.ModelWan3:
		// 2-30 s in whole seconds, 480P / 720P / 1080P, with native sound. First and last frame, or up to 10 reference images (the
		// two modes are exclusive, which Prepare keeps); reference videos and audio, files and web links are not used.
		return Capabilities{
			Durations: WholeSeconds(2, 30), Resolutions: []string{"480p", "720p", "1080p"},
			MaxReferenceImages: 10, FirstFrame: true, LastFrame: true, NativeAudio: true,
		}, true
	case domain.ModelLumaRay32:
		// 5 or 10 s, 360p (draft) to 1080p. First and last frame only in a 5 s clip (the API refuses a frame with 10 s: Prepare
		// keeps to 5); no free reference images (the video type has none: image_ref is for the image models). HDR, looping,
		// editing, extension and reframing are not used.
		return Capabilities{Durations: []float64{5, 10}, Resolutions: []string{"360p", "540p", "720p", "1080p"}, FirstFrame: true, LastFrame: true}, true
	}
	return Capabilities{}, false
}
