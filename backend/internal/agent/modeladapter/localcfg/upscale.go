package localcfg

import "github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/upscale"

// ParamUpscaleRatio is the factor an upscale workflow's ImageScaleBy still applies after its upscale model (upscale.Spec.Ratio).
const ParamUpscaleRatio Param = "upscale_ratio"

func init() { imageParams = append(imageParams, ParamUpscaleRatio) } // imageParams is the list of workflow.go

// IsUpscale reports whether the model is an upscale workflow: one that enlarges a picture, no model to give a prompt to.
func (m Model) IsUpscale() bool { return m.Workflow != nil && m.Workflow.Upscale != nil }

func validateUpscale(where string, media Media, w Workflow, g Graph) []error {
	f := upscale.Facts{Image: media == MediaImage, Prompt: w.Has(ParamPrompt), References: w.Count(ParamReferenceImage), Ratios: w.Count(ParamUpscaleRatio)}
	for _, n := range g {
		f.RunsModel = f.RunsModel || n.ClassType == "ImageUpscaleWithModel"
	}
	return upscale.Validate(where, w.Upscale, f)
}
