package modeladapter

import (
	"fmt"
	"strings"
	"time"

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
	// MaxTime bounds the time the model calls of the job take (Max generation time of the Quality/Cost
	// Policy; 0: no bound). The time starts when the first vendor concurrency slot is held: waiting for
	// a slot is not time the model spent. When it runs out GenerateVideo fails with ErrTimeLimit.
	MaxTime time.Duration
}

// constraintPhrases are the wording of the constraints the Prompt Planner uses most.
var constraintPhrases = map[string]string{
	"preserve_face_identity":        "keep the character's face identical to the reference images",
	"preserve_wardrobe":             "keep the character's wardrobe unchanged",
	"preserve_hairstyle":            "keep the character's hairstyle unchanged",
	"preserve_environment_geometry": "keep the layout of the environment unchanged",
	"preserve_product_geometry":     "keep the product's shape exactly as in the reference images",
	"preserve_logo":                 "keep the logo and label text sharp and undistorted",
	"do_not_add_characters":         "no additional people appear",
}

// negativeTerms are what every prompt asks the model to avoid.
var negativeTerms = []string{"blurry", "distorted face", "extra fingers", "warped text", "flickering", "watermark", "subtitles"}

// Prepare renders the Canonical Shot Spec for one video model: an English prompt, a negative
// prompt for models that take one, the clip length the model supports and the inputs it can use
// (capped by its capabilities).
func Prepare(target VideoTarget, job VideoJob) (VideoRequest, error) {
	caps, ok := Capabilities(target.Model)
	if !ok {
		return VideoRequest{}, fmt.Errorf("modeladapter: %q is not a video model", target.Model)
	}
	req := VideoRequest{
		DurationSeconds: caps.SnapDuration(job.DurationSeconds),
		Resolution:      target.Resolution,
		AspectRatio:     job.AspectRatio,
	}
	if req.AspectRatio == "" {
		req.AspectRatio = "16:9"
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

	body, constraints, avoid := describe(job.Spec, req.DurationSeconds, len(req.References) > 0 || req.FirstFrame != nil)
	switch target.Model {
	case domain.ModelKling3:
		req.Prompt = fit(body, constraints, 2400)
		req.NegativePrompt = domain.TruncateRunes(strings.Join(avoid, ", "), 400)
	case domain.ModelRunwayGen45: // no negative prompt, and a 1000 character limit: Constraints and the avoid list are kept whole
		req.Prompt = fit(body, constraints+avoidSentence(avoid), 1000)
	default: // models without a negative prompt get the constraints as part of the prompt
		req.Prompt = fit(body, constraints+avoidSentence(avoid), 2400)
	}
	return req, nil
}

// fit joins body and tail and keeps the tail whole: when the result would exceed limit, the body is
// shortened. The tail holds what the model must not lose (the Constraints, the Avoid list); the
// body, whose last sentences are the least specific, is what gives way.
func fit(body, tail string, limit int) string {
	if n := limit - len([]rune(tail)); n > 0 {
		return domain.TruncateRunes(body, n) + tail
	}
	return domain.TruncateRunes(strings.TrimSpace(tail), limit)
}

func avoidSentence(avoid []string) string { return " Avoid: " + strings.Join(avoid, ", ") + "." }

// describe writes the spec as prose. It returns the prompt without its Constraints, the Constraints
// sentence (empty when there is none; " Constraints: ..." with its leading space) and the things to avoid.
func describe(s domain.ShotSpec, seconds float64, hasImages bool) (body, constraints string, avoid []string) {
	var b strings.Builder
	fmt.Fprintf(&b, "%s.", strings.TrimRight(strings.TrimSpace(s.ShotObjective), ". "))
	if s.Style != "" {
		fmt.Fprintf(&b, " Visual style: %s.", humanize(s.Style))
	}
	if c := s.Character; c != nil {
		parts := []string{}
		if c.Wardrobe != "" && c.Wardrobe != "inherit_previous" {
			parts = append(parts, "wearing "+humanize(c.Wardrobe))
		}
		if c.Hair != "" && c.Hair != "inherit_previous" {
			parts = append(parts, "hair: "+humanize(c.Hair))
		}
		if len(parts) > 0 {
			fmt.Fprintf(&b, " The character is %s.", strings.Join(parts, ", "))
		}
	}
	if p := s.Product; p != nil && (p.Label != "" || p.Orientation != "") {
		fmt.Fprintf(&b, " The product %s is shown%s%s.", humanize(p.ID),
			ifSet(p.Orientation, ", oriented "), ifSet(p.Label, ", with the label reading "))
	}
	if l := s.Location; l != nil && (l.Time != "" || l.Weather != "") {
		fmt.Fprintf(&b, " Setting:%s%s.", ifSet(l.Time, " time of day "), ifSet(l.Weather, " weather "))
	}
	if len(s.Acting) > 0 {
		b.WriteString(" Action timeline:")
		for _, a := range s.Acting {
			fmt.Fprintf(&b, " %ss: %s;", strings.TrimSuffix(a.Time, "s"), humanize(a.Action))
		}
	}
	cam := []string{}
	if s.Camera.Framing != "" {
		cam = append(cam, humanize(s.Camera.Framing)+" shot")
	}
	if s.Camera.Lens != "" {
		cam = append(cam, s.Camera.Lens+" lens")
	}
	if s.Camera.Movement != "" {
		cam = append(cam, humanize(s.Camera.Movement))
	}
	if len(cam) > 0 {
		fmt.Fprintf(&b, " Camera: %s.", strings.Join(cam, ", "))
	}
	light := []string{}
	if s.Lighting.KeyDirection != "" {
		light = append(light, "key light from "+humanize(s.Lighting.KeyDirection))
	}
	if s.Lighting.Mood != "" {
		light = append(light, humanize(s.Lighting.Mood))
	}
	if s.Lighting.Inherit == "previous_shot" {
		light = append(light, "same lighting as the previous shot")
	}
	if len(light) > 0 {
		fmt.Fprintf(&b, " Lighting: %s.", strings.Join(light, ", "))
	}
	if s.Continuity.StartFromPreviousFinalState {
		b.WriteString(" The shot starts from the final state of the previous shot.")
	}
	fmt.Fprintf(&b, " Duration %.0f seconds, single continuous take.", seconds)
	if hasImages {
		b.WriteString(" Follow the provided reference images for identity, product shape and environment.")
	}
	var must []string
	avoid = append([]string(nil), negativeTerms...)
	for _, c := range s.Constraints {
		if term, ok := strings.CutPrefix(c, "do_not_"); ok {
			if phrase, known := constraintPhrases[c]; known {
				must = append(must, phrase)
			} else {
				must = append(must, "do not "+humanize(term))
			}
			avoid = append(avoid, humanize(term))
			continue
		}
		if phrase, ok := constraintPhrases[c]; ok {
			must = append(must, phrase)
		} else {
			must = append(must, humanize(c))
		}
	}
	if len(must) > 0 {
		constraints = fmt.Sprintf(" Constraints: %s.", strings.Join(must, "; "))
	}
	return b.String(), constraints, avoid
}

func humanize(s string) string { return strings.TrimSpace(strings.ReplaceAll(s, "_", " ")) }

func ifSet(v, prefix string) string {
	if v == "" {
		return ""
	}
	return prefix + humanize(v)
}
