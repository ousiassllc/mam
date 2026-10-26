package localcfg

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// VideoSettings are the video capabilities that cannot be read from a workflow: the clip lengths and
// output sizes the workflow can produce, and whether the model makes sound. The rest (first / last
// frame, how many reference images) follows from the workflow's assignments.
type VideoSettings struct {
	// Durations are the clip lengths in seconds, ascending.
	Durations []float64 `json:"durations"`
	// Resolutions are the output sizes by their short side: "480p", "720p", "1080p".
	Resolutions []string `json:"resolutions"`
	// NativeAudio means the model generates sound together with the picture.
	NativeAudio bool `json:"native_audio,omitempty"`
}

// Model is a model defined on an endpoint: a ComfyUI workflow, or the model identifier of an
// OpenAI-compatible server, with its capabilities and its prices. Its identity is the key it is stored
// under in "local_models" ("<endpoint>/<name>", ID gives the identifier of take.model_name) and never
// changes: a rename is a delete and a create.
type Model struct {
	Media Media `json:"media"`
	// Model is the model identifier an OpenAI-compatible server is asked for (empty for ComfyUI).
	Model string `json:"model,omitempty"`
	// Workflow is the ComfyUI workflow (nil for an OpenAI-compatible model).
	Workflow *Workflow `json:"workflow,omitempty"`
	// AcceptsImageInputs says an OpenAI-compatible image model takes reference images (/v1/images/edits).
	// A ComfyUI model takes them when its workflow has reference_image assignments.
	AcceptsImageInputs bool `json:"accepts_image_inputs,omitempty"`
	// Video is required for a video model.
	Video *VideoSettings `json:"video,omitempty"`
	// CostPerImage and CostPerSecond are the estimated cost in USD of an image and of a second of video.
	// They default to 0: the Cost Ledger still gets a row for every generation.
	CostPerImage  float64 `json:"cost_per_image_usd,omitempty"`
	CostPerSecond float64 `json:"cost_per_second_usd,omitempty"`
}

// ImageInputs reports whether the model uses reference images.
func (m Model) ImageInputs(kind Kind) bool {
	if kind == KindComfyUI {
		return m.Workflow != nil && m.Workflow.Has(ParamReferenceImage)
	}
	return m.AcceptsImageInputs
}

// Caps are the video capabilities of a model, in the field order of modeladapter.VideoCapabilities.
type Caps struct {
	Durations          []float64
	Resolutions        []string
	MaxReferenceImages int
	FirstFrame         bool
	LastFrame          bool
	NativeAudio        bool
}

// VideoCaps are the capabilities of a video model: lengths, sizes and sound from its settings, first /
// last frame and the number of reference images from the assignments of its workflow. ok is false for
// anything that is not a video model.
func (m Model) VideoCaps() (Caps, bool) {
	if m.Media != MediaVideo || m.Video == nil || m.Workflow == nil {
		return Caps{}, false
	}
	w := m.Workflow
	return Caps{
		Durations: m.Video.Durations, Resolutions: m.Video.Resolutions, MaxReferenceImages: w.Count(ParamReferenceImage),
		FirstFrame: w.Has(ParamFirstFrame), LastFrame: w.Has(ParamLastFrame), NativeAudio: m.Video.NativeAudio,
	}, true
}

var resolutionPattern = regexp.MustCompile(`^[1-9][0-9]{2,3}p$`)

// ShortSide is the number of pixels of the short side a resolution such as "720p" names.
func ShortSide(resolution string) (int, bool) {
	if !resolutionPattern.MatchString(resolution) {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSuffix(resolution, "p"))
	return n, err == nil
}

// DefaultSizeMultiple is the multiple the width and height of a video are rounded to (video models
// take sizes divisible by 16, 32 or 64).
const DefaultSizeMultiple = 16

// VideoSize is the width and height of a video of the resolution (its short side) and the aspect ratio
// ("16:9"), each rounded to a multiple of multiple (0 selects DefaultSizeMultiple).
func VideoSize(resolution, aspect string, multiple int) (width, height int) {
	short, ok := ShortSide(resolution)
	if !ok {
		short = 720
	}
	if multiple <= 0 {
		multiple = DefaultSizeMultiple
	}
	wr, hr := 16, 9
	if a, b, found := strings.Cut(aspect, ":"); found {
		if x, err := strconv.Atoi(a); err == nil && x > 0 {
			if y, err := strconv.Atoi(b); err == nil && y > 0 {
				wr, hr = x, y
			}
		}
	}
	round := func(v float64) int { return max(multiple, int(v/float64(multiple)+0.5)*multiple) }
	if wr >= hr {
		return round(float64(short) * float64(wr) / float64(hr)), round(float64(short))
	}
	return round(float64(short)), round(float64(short) * float64(hr) / float64(wr))
}

// ValidateModel checks one entry of "local_models" against the endpoints; a nil result means the model
// can be used. Problems are listed in a fixed order.
func ValidateModel(key string, m Model, endpoints map[string]Endpoint) []error {
	where := "local_models " + key
	endpoint, _, ok := SplitKey(key)
	if !ok {
		return []error{fmt.Errorf("%s: the key must be <endpoint>/<name>, each of a-z, 0-9, '.', '_' or '-'", where)}
	}
	ep, ok := endpoints[endpoint]
	if !ok {
		return []error{fmt.Errorf("%s: the endpoint %q is not registered", where, endpoint)}
	}
	var errs []error
	if m.Media != MediaImage && m.Media != MediaVideo {
		errs = append(errs, fmt.Errorf("%s: media must be image or video", where))
	}
	if m.CostPerImage < 0 || m.CostPerSecond < 0 {
		errs = append(errs, fmt.Errorf("%s: costs must not be negative", where))
	}
	switch ep.Kind {
	case KindComfyUI:
		errs = append(errs, validateComfyModel(where, m)...)
	case KindOpenAICompat:
		errs = append(errs, validateCompatModel(where, m)...)
	}
	if m.Media == MediaVideo {
		errs = append(errs, validateVideoSettings(where, m.Video)...)
	} else if m.Video != nil {
		errs = append(errs, fmt.Errorf("%s: video settings belong to a video model", where))
	}
	return errs
}

func validateComfyModel(where string, m Model) []error {
	switch {
	case m.Model != "" || m.AcceptsImageInputs:
		return []error{fmt.Errorf("%s: a ComfyUI model is its workflow: model and accepts_image_inputs do not apply", where)}
	case m.Workflow == nil:
		return []error{fmt.Errorf("%s: a ComfyUI model needs a workflow", where)}
	case m.Media != MediaImage && m.Media != MediaVideo:
		return nil
	}
	return validateWorkflow(where, m.Media, *m.Workflow)
}

func validateCompatModel(where string, m Model) []error {
	var errs []error
	if m.Media == MediaVideo {
		errs = append(errs, fmt.Errorf("%s: an OpenAI-compatible endpoint serves images only (video goes through ComfyUI)", where))
	}
	if strings.TrimSpace(m.Model) == "" || len(m.Model) > 200 {
		errs = append(errs, fmt.Errorf("%s: model (the model identifier of the server) is required", where))
	}
	if m.Workflow != nil {
		errs = append(errs, fmt.Errorf("%s: an OpenAI-compatible model has no workflow", where))
	}
	return errs
}

func validateVideoSettings(where string, v *VideoSettings) []error {
	if v == nil {
		return []error{fmt.Errorf("%s: a video model needs video settings (durations and resolutions)", where)}
	}
	var errs []error
	if len(v.Durations) == 0 || !slices.IsSorted(v.Durations) || v.Durations[0] <= 0 || len(v.Durations) > 64 {
		errs = append(errs, fmt.Errorf("%s: video.durations must be seconds, positive and ascending", where))
	}
	if len(v.Resolutions) == 0 || len(v.Resolutions) > 16 {
		errs = append(errs, fmt.Errorf("%s: video.resolutions needs at least one entry such as \"720p\"", where))
	}
	for _, r := range v.Resolutions {
		if _, ok := ShortSide(r); !ok {
			errs = append(errs, fmt.Errorf("%s: video.resolutions: %q is not a size such as \"720p\"", where, r))
		}
	}
	return errs
}

// IDs are the identifiers (ID) of the models of the given media, in key order. A model whose endpoint is
// not registered is left out.
func IDs(endpoints map[string]Endpoint, models map[string]Model, media Media) []string {
	var ids []string
	for _, key := range slices.Sorted(maps.Keys(models)) {
		endpoint, name, ok := SplitKey(key)
		if ep, registered := endpoints[endpoint]; ok && registered && models[key].Media == media {
			ids = append(ids, ID(ep.Kind, endpoint, name))
		}
	}
	return ids
}
