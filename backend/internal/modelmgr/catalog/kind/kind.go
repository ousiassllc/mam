// Package kind is the catalog's third kind of model besides image and video: an upscale model enlarges a picture (Issue #975). It is a
// package of its own so that package catalog stays under the line limit.
package kind

import "github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"

// Upscale is the kind of a model whose workflows are the upscale workflows of the templates (localcfg.Workflow.Upscale): image workflows
// on the bundled endpoint that the generation test's "make it larger" runs. They are not models to give a prompt to.
const Upscale localcfg.Media = "upscale"

// Valid reports whether k is a kind a catalog model may have.
func Valid(k localcfg.Media) bool {
	return k == localcfg.MediaImage || k == localcfg.MediaVideo || k == Upscale
}

// Fits reports whether a template that makes media (and is an upscale workflow or not) is one a model of kind k may start from: an upscale
// model's workflows are the upscale (image) workflows, and no other model uses one.
func Fits(k, media localcfg.Media, upscale bool) bool {
	if k == Upscale {
		return media == localcfg.MediaImage && upscale
	}
	return media == k && !upscale
}
