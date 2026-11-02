package catalog

import "github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"

// KindUpscale is the kind of a model that enlarges a picture (an upscale model, or an upscale model with the diffusion model that
// finishes the picture): its workflows are the upscale workflows of the templates (localcfg.Workflow.Upscale), made on the bundled
// endpoint as image workflows that the generation test's "make it larger" runs. They are not models to give a prompt to.
const KindUpscale localcfg.Media = "upscale"

// TemplateMedia is the media of the templates the model's workflows start from: an upscale model's workflows are image workflows.
func (m Model) TemplateMedia() localcfg.Media {
	if m.Kind == KindUpscale {
		return localcfg.MediaImage
	}
	return m.Kind
}
