package modeladapter

import (
	"fmt"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/shotprompt"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/textutil"
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
	// MaxTime bounds the time the model calls of the job take (Max generation time of the Quality/Cost
	// Policy; 0: no bound). The time starts when the first vendor concurrency slot is held: waiting for
	// a slot is not time the model spent. When it runs out GenerateVideo fails with ErrTimeLimit.
	MaxTime time.Duration
}

// Prepare renders the Canonical Shot Spec for one video model: an English prompt, a negative
// prompt for models that take one, the clip length the model supports and the inputs it can use
// (capped by its capabilities).
func Prepare(target VideoTarget, job VideoJob) (VideoRequest, error) {
	return publishedConfig().Prepare(target, job)
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
	useReferences := job.PreferReferences && caps.MaxReferenceImages > 0 && len(job.References) > 0
	if caps.FirstFrame && !useReferences {
		req.FirstFrame = job.FirstFrame
	}
	if caps.LastFrame && !useReferences {
		req.LastFrame = job.LastFrame
	}
	// A model reads either the frames or the free references, never both: Seedance 2.0 treats first
	// frame, first-and-last frame and reference-to-video as mutually exclusive scenarios and rejects
	// a request that mixes them. The frames win here unless the job prefers the references (Reference
	// Reinforcement). MaxReferenceImages zero means the model only takes frames (Kling, Runway).
	if req.FirstFrame == nil {
		if n := min(caps.MaxReferenceImages, len(job.References)); n > 0 {
			req.References = job.References[:n]
		}
	}

	body, constraints, avoid := shotprompt.Describe(job.Spec, req.DurationSeconds, len(req.References) > 0 || req.FirstFrame != nil)
	switch target.Model {
	case domain.ModelKling3, c.negativeModel(target.Model):
		req.Prompt = shotprompt.Fit(body, constraints, 2400)
		req.NegativePrompt = textutil.TruncateRunes(strings.Join(avoid, ", "), 400)
	case domain.ModelRunwayGen45: // no negative prompt, and a 1000 character limit: Constraints and the avoid list are kept whole
		req.Prompt = shotprompt.Fit(body, constraints+shotprompt.AvoidSentence(avoid), 1000)
	default: // models without a negative prompt get the constraints as part of the prompt
		req.Prompt = shotprompt.Fit(body, constraints+shotprompt.AvoidSentence(avoid), 2400)
	}
	return req, nil
}
