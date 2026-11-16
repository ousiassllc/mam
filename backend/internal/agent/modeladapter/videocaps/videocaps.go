// Package videocaps holds what each built-in cloud video model can do (spec/spec.md §11 "モデル能力"): the data behind
// modeladapter.Capabilities, a package of its own because modeladapter is at its size limit. modeladapter.VideoCapabilities is
// defined as this package's Capabilities (a defined type with the same fields), so that one converts to the other.
package videocaps

import (
	"fmt"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

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
	// AudioInput means the model is given a sound file (speech to video: Wan 2.2 S2V; VideoRequest.Audio).
	AudioInput bool
	// AudioLength means the clip is as long as the sound the request carries (Wan 2.2 S2V with Extend): Durations are only informative, SnapDuration leaves the length as it is, and
	// the length is read from the sound (direct.Video), never taken from the request.
	AudioLength bool
}

// SnapDuration returns the shortest supported length that holds the requested seconds, or the longest supported length when the request is longer than any
// of them (the Editor trims later). A model whose clip is as long as its sound (AudioLength) has no lengths to snap to: the seconds stay as they are.
func (c Capabilities) SnapDuration(seconds float64) float64 {
	if c.AudioLength {
		return seconds
	}
	if i := slices.IndexFunc(c.Durations, func(d float64) bool { return d >= seconds-1e-9 }); i >= 0 {
		return c.Durations[i]
	}
	return c.Durations[len(c.Durations)-1]
}

// CheckInterpolation refuses (apperr input_unsupported, HTTP 422) a clip with both a first and a last frame (both) that is longer than
// Gemini Omni Flash makes from two frames, 10 seconds (the official Omni guide, "Limitations": the API returns invalid argument; one
// image or text goes up to 15 seconds). The length is not shortened and no frame is dropped. Any other request or model passes (nil).
func CheckInterpolation(m domain.ModelName, seconds float64, both bool) error {
	const limit = 10
	if m != domain.ModelGeminiOmniFlash || !both || seconds <= limit {
		return nil
	}
	return apperr.New(apperr.CodeInputUnsupported).
		WithCause(fmt.Sprintf("最初と最後のフレームの両方を使うと、Gemini Omni Flash は %d 秒までの動画しか作れません（指定は %g 秒）。長さを %d 秒以下にするか、どちらかのフレームを外してください。", limit, seconds, limit)).
		WithDetail(fmt.Sprintf("%s makes at most %d seconds from a first and a last frame, the request is %g seconds", m, limit, seconds))
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
		// 3-15 s in whole seconds, 24 FPS, with native sound. 360p and 4K exist too, but only 720p and 1080p (upscaled) are offered:
		// they are the sizes of a video of a Shot. First and last frame, or free reference images (the two are exclusive in Prepare); the length is sent as
		// response_format.duration. The two frames make up to 10 s only (CheckInterpolation refuses more). The official documentation states a limit of 6 reference images.
		return Capabilities{
			Durations: WholeSeconds(3, 15), Resolutions: []string{"720p", "1080p"},
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
