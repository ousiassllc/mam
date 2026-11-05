// Package kind is the catalog's kinds of model besides image and video: an upscale model enlarges a picture (Issue #975), a video upscale
// model makes a video larger and a video interpolation model makes it smoother (Issue #1031). It is a package of its own so that package
// catalog stays under the line limit.
package kind

import (
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/videoenhance"
)

// Upscale is the kind of a model whose workflows are the upscale workflows of the templates (localcfg.Workflow.Upscale): image workflows
// on the bundled endpoint that the generation test's "make it larger" runs. They are not models to give a prompt to.
const Upscale localcfg.Media = "upscale"

// VideoUpscale is the kind of a model whose workflows are the video upscale workflows of the templates (localcfg.Workflow.VideoEnhance with
// the operation upscale): video workflows that make an existing video larger. They are not models to give a prompt to.
const VideoUpscale localcfg.Media = "video_upscale"

// VideoInterpolate is the kind of a model whose workflows are the frame interpolation workflows of the templates (localcfg.Workflow.VideoEnhance
// with the operation interpolate): video workflows that make an existing video smoother. They are not models to give a prompt to.
const VideoInterpolate localcfg.Media = "video_interpolate"

// Valid reports whether k is a kind a catalog model may have.
func Valid(k localcfg.Media) bool {
	return k == localcfg.MediaImage || k == localcfg.MediaVideo || k == Upscale || k == VideoUpscale || k == VideoInterpolate
}

// IsVideoEnhance reports whether k is the kind of a model that enhances an existing video.
func IsVideoEnhance(k localcfg.Media) bool { return k == VideoUpscale || k == VideoInterpolate }

// Fits reports whether a template (the model it makes) is one a model of kind k may start from: an upscale model's workflows are the upscale
// (image) workflows, a video upscale model's the video enhancement workflows that upscale, a video interpolation model's the ones that
// interpolate, and no other model uses one of those.
func Fits(k localcfg.Media, t localcfg.Model) bool {
	switch k {
	case Upscale:
		return t.Media == localcfg.MediaImage && t.IsUpscale()
	case VideoUpscale:
		return t.IsVideoEnhance() && t.Workflow.VideoEnhance.Op == videoenhance.Upscale
	case VideoInterpolate:
		return t.IsVideoEnhance() && t.Workflow.VideoEnhance.Op == videoenhance.Interpolate
	}
	return t.Media == k && !t.IsUpscale() && !t.IsVideoEnhance()
}
