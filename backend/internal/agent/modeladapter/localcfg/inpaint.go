package localcfg

import (
	"encoding/json"
	"fmt"
)

// Partial redraw (inpaint): one reference_image, one mask_image (white redraws, black keeps) and denoise (the graph's number is the default).
const (
	ParamMaskImage Param         = "mask_image"
	ParamDenoise   Param         = "denoise"
	ModeInpaint    ReferenceMode = "inpaint"
)

func init() { imageParams = append(imageParams, ParamMaskImage, ParamDenoise) }

func inpaintErrs(where string, w Workflow, g Graph) []error {
	if w.Count(ParamReferenceImage) != 1 || w.Count(ParamMaskImage) != 1 || !w.Has(ParamDenoise) {
		return []error{fmt.Errorf("%s: an inpaint workflow needs one reference_image, one mask_image and a denoise assignment", where)}
	}
	var errs []error
	for i, a := range w.Assignments {
		var f float64
		if a.Param == ParamDenoise && (json.Unmarshal(g[a.Node].Inputs[a.Input], &f) != nil || !(f > 0 && f <= 1)) {
			errs = append(errs, fmt.Errorf("%s: assignments[%d]: denoise must be a number from above 0 to 1", where, i))
		}
	}
	return errs
}
