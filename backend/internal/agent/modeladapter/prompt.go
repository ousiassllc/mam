package modeladapter

import (
	"fmt"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/shotprompt"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/viaprovider"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/videocaps"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// VideoJob is a model-independent request for the video of one Shot: the Canonical Shot Spec
// and the images available for it. Prepare turns it into the request of one model.
type VideoJob struct {
	Spec            domain.ShotSpec
	DurationSeconds float64
	AspectRatio     string
	// FirstFrame is the Keyframe the clip starts from, LastFrame the frame it ends on.
	FirstFrame, LastFrame *Input
	// References are the Reference Pack images in priority order (the first ones are kept when
	// the model accepts fewer).
	References []Input
	// PreferReferences asks for the References instead of the frames (Reference Reinforcement,
	// where the references are what is being reinforced). Prepare decides per model: a model that
	// takes free reference images reads them and leaves the frames out, any other model keeps
	// its frames, so the fail over model gets the input it can use.
	PreferReferences bool
	Audio            *Input  // the sound of a Shot with audio (Issue #1153): the job goes only to a model that takes it (videocaps.CheckAudio)
	AudioSeconds     float64 // the length of Audio, measured by the caller: the clip of a model whose clip is as long as its sound is this long
	// MaxTime bounds the time the model calls of the job take (Max generation time of the Quality/Cost
	// Policy; 0: no bound). The time starts when the first vendor concurrency slot is held: waiting for
	// a slot is not time the model spent. When it runs out GenerateVideo fails with ErrTimeLimit.
	MaxTime time.Duration
}

// Prepare renders the Canonical Shot Spec for one video model: an English prompt, a negative
// prompt for models that take one, the clip length the model supports and the inputs it can use
// (capped by its capabilities). Gemini Omni Flash with two frames over 10 seconds is refused (videocaps.CheckInterpolation).
func Prepare(target VideoTarget, job VideoJob) (VideoRequest, error) {
	return publishedConfig().Prepare(target, job)
}

// ClipSeconds is the length of the clip Prepare asks the model for when the Shot is the given seconds long: the shortest
// supported length, and for Luma Ray 3.2 a clip with a frame is 5 seconds long. framed says whether the clip takes a first or
// last frame. The estimates of the strategy use it so they match what Prepare requests.
func (c Config) ClipSeconds(m domain.ModelName, seconds float64, framed bool) float64 {
	caps, ok := c.VideoCaps(m)
	if !ok {
		return seconds
	}
	return clipSeconds(m, caps.SnapDuration(seconds), framed)
}

// Prepare is the package function for the models of this setting.
func (c Config) Prepare(target VideoTarget, job VideoJob) (VideoRequest, error) {
	caps, ok := c.VideoCaps(target.Model)
	if !ok {
		return VideoRequest{}, fmt.Errorf("modeladapter: %q is not a video model", target.Model)
	}
	req := VideoRequest{
		DurationSeconds: caps.SnapDuration(job.DurationSeconds),
		Resolution:      target.Resolution,
		AspectRatio:     job.AspectRatio,
	}
	if req.AspectRatio == "" {
		req.AspectRatio = domain.DefaultAspectRatio
	}
	if job.Audio != nil {
		if err := videocaps.CheckAudio(target.Model, videocaps.Capabilities(caps)); err != nil {
			return VideoRequest{}, err
		}
		req.Audio, req.DurationSeconds = job.Audio, videocaps.SoundClip(videocaps.Capabilities(caps), req.DurationSeconds, job.AudioSeconds)
	}
	useReferences := job.PreferReferences && caps.MaxReferenceImages > 0 && len(job.References) > 0
	if caps.FirstFrame && !useReferences {
		req.FirstFrame = job.FirstFrame
	}
	if caps.LastFrame && !useReferences {
		req.LastFrame = job.LastFrame
	}
	req.DurationSeconds = clipSeconds(target.Model, req.DurationSeconds, req.FirstFrame != nil || req.LastFrame != nil)
	// A model reads either the frames or the free references, never both: Seedance 2.0 treats first
	// frame, first-and-last frame and reference-to-video as mutually exclusive scenarios and rejects
	// a request that mixes them. The frames win here unless the job prefers the references (Reference
	// Reinforcement). MaxReferenceImages zero means the model only takes frames (Kling, Runway).
	if req.FirstFrame == nil {
		if n := min(caps.MaxReferenceImages, len(job.References)); n > 0 {
			req.References = job.References[:n]
		}
	}
	// A model of a sound takes one picture to speak or sing in and has no first frame to put the Keyframe in: with no free reference the Keyframe is that picture.
	if req.Audio != nil && req.FirstFrame == nil && len(req.References) == 0 && caps.MaxReferenceImages > 0 && job.FirstFrame != nil {
		req.References = []Input{*job.FirstFrame}
	}

	body, constraints, avoid := shotprompt.Describe(job.Spec, req.DurationSeconds, len(req.References) > 0 || req.FirstFrame != nil)
	if target.Model == domain.ModelKling3 || target.Model == c.negativeModel(target.Model) {
		req.Prompt, req.NegativePrompt = shotprompt.FitNegative(body, constraints, avoid, 2400, 400, viaprovider.FoldsNegative(c.VideoProvider, target.Model))
	} else { // no negative prompt field: the Constraints and the avoid list are part of the prompt, within the model's limit
		req.Prompt = shotprompt.Fit(body, constraints+shotprompt.AvoidSentence(avoid), shotprompt.PromptLimit(target.Model))
	}
	return req, videocaps.CheckInterpolation(target.Model, req.DurationSeconds, req.FirstFrame != nil && req.LastFrame != nil)
}
