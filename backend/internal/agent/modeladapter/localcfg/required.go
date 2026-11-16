package localcfg

import "slices"

// The inputs RequiredImages names (the words of the generation test's input image roles).
const (
	RequiredFirstFrame = "first_frame"
	RequiredLastFrame  = "last_frame"
	RequiredReference  = "reference"
	RequiredAudio      = "audio"
)

// RequiredImages are the input images the workflow cannot run without (the adapter refuses a request that lacks one: comfyui's missing):
// "first_frame" and "last_frame" when the workflow has that assignment, and "reference" when a reference slot is not optional and the workflow
// does not list what a run without a reference changes (NoReference).
func (w Workflow) RequiredImages() []string {
	var out []string
	add := func(r string) {
		if !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	for _, a := range w.Assignments {
		switch {
		case a.Param == ParamFirstFrame:
			add(RequiredFirstFrame)
		case a.Param == ParamLastFrame:
			add(RequiredLastFrame)
		case a.Param == ParamAudio:
			add(RequiredAudio)
		case a.Param == ParamReferenceImage && !a.Optional && len(w.NoReference) == 0:
			add(RequiredReference)
		}
	}
	return out
}
