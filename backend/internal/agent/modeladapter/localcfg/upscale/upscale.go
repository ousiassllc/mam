// Package upscale is what a ComfyUI workflow says about itself when it enlarges a picture instead of making one (spec/architecture/gentest/
// gentest.md §3.4, local-models.md §4.5): the factor of the upscale model it loads, how it finishes the picture, how large its output may
// be, and the check that the graph it marks fits that. It is a package of its own, like promptweight, so that package localcfg stays
// under the line limit; it knows no workflow type of localcfg (the graph is told in Facts).
package upscale

import "fmt"

// Mode is how an upscale workflow makes the larger picture.
type Mode string

// The modes of an upscale workflow.
const (
	// Model enlarges with an upscale model (UpscaleModelLoader and ImageUpscaleWithModel, which cuts the picture into tiles itself) and
	// nothing else: the picture is the model's, no sampling.
	Model Mode = "model"
	// TwoPass enlarges like Model and then samples the picture again with a low denoise (tiled VAE), so that the details the upscale
	// model made up are drawn by the diffusion model. It takes a prompt.
	TwoPass Mode = "two_pass"
)

// Spec marks a workflow as one that enlarges a picture: it is not a model to choose for a prompt (it is left out of the models of the
// generation test and the trial generation), and the generation test's "make it larger" runs it.
type Spec struct {
	// Label is what the screen calls the workflow (the chooser of "make it larger").
	Label string `json:"label"`
	// Native is the factor the upscale model the workflow loads enlarges by (2 or 4 for the models of the catalog).
	Native int `json:"native"`
	// Mode is how the workflow makes the picture.
	Mode Mode `json:"mode"`
	// MaxOutputPixels is the most pixels (width × height) the workflow makes: 0 is no limit of its own (the generation test has one).
	MaxOutputPixels int `json:"max_output_pixels,omitempty"`
}

// Ratio is the value of the workflow's upscale_ratio for the requested scale: the factor an ImageScaleBy still applies to the output of
// the upscale model (a 4x model asked for 2x ends at half its size). A scale of 0 is the model's own.
func (s Spec) Ratio(scale float64) float64 {
	if scale <= 0 || s.Native <= 0 {
		return 1
	}
	return scale / float64(s.Native)
}

// Facts are what the graph and the assignments of the marked workflow are.
type Facts struct {
	Image     bool // the workflow makes images
	Prompt    bool // it has a prompt assignment
	RunsModel bool // its graph has an ImageUpscaleWithModel
	// References and Ratios are how many reference_image and upscale_ratio assignments it has.
	References, Ratios int
}

// Validate checks the marker of a workflow (where is the prefix of the messages): spec nil is a workflow that is not one, which takes no
// ratio. A marked workflow is an image workflow, with a known factor, label and mode, the picture to enlarge and the ratio put into
// the graph, a prompt when it samples, and an upscale model in the graph.
func Validate(where string, spec *Spec, f Facts) []error {
	if spec == nil {
		if f.Ratios > 0 {
			return []error{fmt.Errorf("%s: upscale_ratio belongs to an upscale workflow (set \"upscale\")", where)}
		}
		return nil
	}
	var errs []error
	add := func(format string, a ...any) { errs = append(errs, fmt.Errorf(where+": "+format, a...)) }
	switch {
	case !f.Image:
		add("only an image workflow enlarges pictures (upscale)")
	case spec.Label == "":
		add("upscale label is empty")
	case spec.Native < 2 || spec.Native > 8:
		add("upscale native must be the factor of the upscale model, 2 to 8")
	case spec.MaxOutputPixels < 0:
		add("upscale max_output_pixels cannot be negative")
	}
	switch {
	case spec.Mode == Model:
	case spec.Mode == TwoPass && f.Prompt:
	case spec.Mode == TwoPass:
		add("a two-pass upscale takes a prompt")
	default:
		add("upscale mode must be %q or %q", Model, TwoPass)
	}
	if f.References != 1 || f.Ratios != 1 {
		add("an upscale workflow takes exactly one reference_image (the picture) and one upscale_ratio")
	}
	if !f.RunsModel {
		add("an upscale workflow runs an upscale model (ImageUpscaleWithModel)")
	}
	return errs
}
