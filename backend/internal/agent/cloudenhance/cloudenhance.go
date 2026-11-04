// Package cloudenhance is what "make a video larger" and "make a video smoother" (Issue #1031) share between the local ComfyUI workflows
// (SeedVR2, RIFE) and the paid cloud (Topaz Video API): the operations, the facts of the source video, what is asked of the output, and the
// Cloud interface the cloud provider implements. It knows no ComfyUI and no HTTP.
package cloudenhance

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
)

// Op is what is done to the video.
type Op string

// The operations.
const (
	// OpUpscale makes the picture larger (SeedVR2 3B locally; Proteus, Iris, Starlight at Topaz).
	OpUpscale Op = "upscale"
	// OpInterpolate makes the video smoother with more frames (RIFE locally; Apollo, Chronos at Topaz).
	OpInterpolate Op = "interpolate"
)

// Provider is where the work is done.
type Provider string

// The providers.
const (
	ProviderLocal Provider = "local"
	ProviderTopaz Provider = "topaz"
)

// Source is the facts of the source video, read from the file (ffprobe), never from what was asked of the generation.
type Source struct {
	Container  string  // lower case extension without the dot: "mp4"
	SizeBytes  int64   // size of the file
	Duration   float64 // seconds
	FrameCount int
	FPS        float64
	Width      int
	Height     int
	HasAudio   bool
}

// Params is what is asked of the output. Which fields count depends on Op.
type Params struct {
	Op Op
	// Scale (OpUpscale) is the factor of the width and the height (the short side grows by it). 0: TargetShortSide counts.
	Scale float64
	// TargetShortSide (OpUpscale) is the length in pixels the shorter side ends at (720, 1080): 480 × 848 asked for 720 ends at 720 × 1272.
	TargetShortSide int
	// Multiplier (OpInterpolate) is how many times the frames are: 2 or 4. 0: TargetFPS counts.
	Multiplier int
	// TargetFPS (OpInterpolate) is the frame rate of the output. A local model only makes an integer multiple of the source's.
	TargetFPS float64
}

// CloudModel is a model a cloud provider offers.
type CloudModel struct {
	ID          string // the provider's name of the model ("slp-2.6")
	Provider    Provider
	Op          Op
	Label       string
	Description string
	License     modellicense.Info
}

// CloudRequest is one job to estimate or to run at the provider.
type CloudRequest struct {
	Model  string
	Source Source
	Params Params
}

// CloudEstimate is the provider's quote for a CloudRequest. It is what the user confirms. Credits is the upper bound if the provider
// gives a range. Nothing is charged by getting it.
type CloudEstimate struct {
	RequestID  string // the provider's id of the job, kept to run exactly what was quoted
	Credits    float64
	CreditsMin float64
	Width      int // size and rate of the output the provider will make
	Height     int
	FPS        float64
}

// Progress is how far a cloud job is.
type Progress struct {
	Status  string  // the provider's status, lower case ("processing")
	Percent float64 // 0..100; -1 when the provider does not say
}

// RunInput is one cloud job to run.
type RunInput struct {
	RequestID  string // from CloudEstimate
	SourcePath string // the source video file to upload (streamed from the file, never read whole into memory)
	DestPath   string // where the result is written (a new file; the caller moves it into place and removes it on error)
}

// Cloud is a paid cloud provider of video enhancement. The implementation never writes a key into an error, a log or a result.
type Cloud interface {
	// Configured is whether the provider has the API key it needs (a provider without one is not offered).
	Configured() bool
	// Models are the models the provider offers.
	Models() []CloudModel
	// Estimate asks the provider for the quote (no charge). It fails when the quote cannot be had: the job must not run then.
	Estimate(ctx context.Context, r CloudRequest) (CloudEstimate, error)
	// Run does what Estimate quoted: accepts it (the credits are reserved), uploads the source, waits and downloads the result to
	// DestPath. It returns the size of the result in bytes.
	Run(ctx context.Context, in RunInput, progress func(Progress)) (int64, error)
}
