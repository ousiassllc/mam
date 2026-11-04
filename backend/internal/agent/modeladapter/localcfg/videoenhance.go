package localcfg

import "github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/videoenhance"

// VideoEnhance marks a Workflow that makes an existing video larger or smoother instead of generating one (package videoenhance).
type VideoEnhance = videoenhance.Spec

// The values mam puts into a video enhancement workflow (they are allowed in a marked video workflow only, videoenhance.Validate).
const (
	// ParamSourceVideo is the file name of the video uploaded to the server (the file of a LoadVideo).
	ParamSourceVideo Param = "source_video"
	// ParamVideoScale is the factor an upscale workflow scales the frames by (the multiplier of a ResizeImageMaskNode).
	ParamVideoScale Param = "video_scale"
	// ParamFrameMultiplier is how many frames an interpolation workflow makes of one (the multiplier of a FrameInterpolate).
	ParamFrameMultiplier Param = "frame_multiplier"
	// ParamOutputFPS is the frame rate of the video an interpolation workflow makes (the fps of a CreateVideo).
	ParamOutputFPS Param = "output_fps"
)

func init() {
	videoParams = append(videoParams, ParamSourceVideo, ParamVideoScale, ParamFrameMultiplier, ParamOutputFPS)
} // videoParams is the list of workflow.go

// IsVideoEnhance reports whether the model is a video enhancement workflow: one that makes a video larger or smoother, no model to give a prompt to.
func (m Model) IsVideoEnhance() bool { return m.Workflow != nil && m.Workflow.VideoEnhance != nil }

func validateVideoEnhance(where string, media Media, w Workflow, g Graph) []error {
	f := videoenhance.Facts{
		Video: media == MediaVideo, Sources: w.Count(ParamSourceVideo), Scales: w.Count(ParamVideoScale),
		Multipliers: w.Count(ParamFrameMultiplier), FPSes: w.Count(ParamOutputFPS),
	}
	for _, n := range g {
		f.LoadsVideo = f.LoadsVideo || n.ClassType == "LoadVideo"
	}
	return videoenhance.Validate(where, w.VideoEnhance, f)
}
