// Package videoenhance is what a ComfyUI workflow says about itself when it makes an existing video larger or smoother instead of
// generating one (Issue #1031): SeedVR2 (upscale) and RIFE (interpolate). It is a package of its own, like upscale, so that package
// localcfg stays under the line limit; it knows no workflow type of localcfg.
package videoenhance

import "fmt"

// Op is what the workflow does to the video.
type Op string

// The operations of a video enhancement workflow.
const (
	Upscale     Op = "upscale"
	Interpolate Op = "interpolate"
)

// Spec marks a workflow as one that enhances a video: it is not a model to choose for a prompt (it is left out of every model list), and
// "make the video larger / smoother" runs it.
type Spec struct {
	// Label is what the screen calls the workflow.
	Label string `json:"label"`
	// Op is what it does.
	Op Op `json:"op"`
	// MaxOutputPixels is the most pixels (width × height) of one frame it makes: 0 is no limit of its own.
	MaxOutputPixels int `json:"max_output_pixels,omitempty"`
}

// Facts are what the graph and the assignments of the workflow are.
type Facts struct {
	Video      bool // the workflow makes videos (media video)
	LoadsVideo bool // its graph has a LoadVideo
	// Sources, Scales, Multipliers and FPSes are how many source_video, video_scale, frame_multiplier and output_fps assignments it has.
	Sources, Scales, Multipliers, FPSes int
}

// Validate checks the marker of a workflow (where is the prefix of the messages): spec nil is a workflow that is not one, which takes none
// of the values. A marked workflow is a video workflow with a known label and operation, the video to enhance (one source_video into a
// LoadVideo) and the values its operation puts into the graph: an upscale the scale, an interpolation the multiplier and the frame rate.
// It takes no prompt, and no clip length or size (the source video has them).
func Validate(where string, spec *Spec, f Facts) []error {
	if spec == nil {
		if f.Sources+f.Scales+f.Multipliers+f.FPSes > 0 {
			return []error{fmt.Errorf("%s: source_video, video_scale, frame_multiplier and output_fps belong to a video enhancement workflow (set \"video_enhance\")", where)}
		}
		return nil
	}
	var errs []error
	add := func(format string, a ...any) { errs = append(errs, fmt.Errorf(where+": "+format, a...)) }
	switch {
	case !f.Video:
		add("only a video workflow enhances videos (video_enhance)")
	case spec.Label == "":
		add("video_enhance label is empty")
	case spec.MaxOutputPixels < 0:
		add("video_enhance max_output_pixels cannot be negative")
	}
	if f.Sources != 1 || !f.LoadsVideo {
		add("a video enhancement workflow takes exactly one source_video (the file of a LoadVideo)")
	}
	switch spec.Op {
	case Upscale:
		if f.Scales != 1 || f.Multipliers+f.FPSes > 0 {
			add("an upscale video workflow takes exactly one video_scale and neither frame_multiplier nor output_fps")
		}
	case Interpolate:
		if f.Multipliers != 1 || f.FPSes != 1 || f.Scales > 0 {
			add("an interpolation video workflow takes exactly one frame_multiplier and one output_fps and no video_scale")
		}
	default:
		add("video_enhance op must be %q or %q", Upscale, Interpolate)
	}
	return errs
}
