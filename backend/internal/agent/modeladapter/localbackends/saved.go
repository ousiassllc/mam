package localbackends

import (
	"fmt"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyui"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localhttp"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// buildSaved adds the backend of one version of a saved workflow: the ComfyUI backend with that version's graph and
// assignments, on the endpoint the workflow names.
func buildSaved(b modeladapter.Backends, id domain.ModelName, w localcfg.SavedWorkflow, client *localhttp.Client, prepare PrepareFunc) {
	backend := &comfyui.Backend{Client: client, Model: w.Model}
	if w.Model.Media == localcfg.MediaVideo {
		b.Video[id] = wrapVideo(w.Endpoint, prepare, backend)
	} else {
		b.Image[id] = wrapImage(w.Endpoint, prepare, backend)
	}
}

// savedLists splits the saved workflows for publication: the identifiers to offer for selection (images, videos) and
// the display name of each version. A video workflow is offered at its current version and at an older one that a
// Shot is still pinned to: that Shot's Video Model lock must stay valid after the workflow was edited.
func savedLists(workflows map[string]localcfg.SavedWorkflow) (images, videos []string, labels map[string]string) {
	labels = make(map[string]string, len(workflows))
	for id, w := range workflows {
		labels[id] = fmt.Sprintf("%s (v%d)", w.Name, w.Version)
		switch {
		case w.Model.Media == localcfg.MediaVideo && (w.Current || w.Pinned):
			videos = append(videos, id)
		case w.Model.Media == localcfg.MediaImage && w.Current:
			images = append(images, id)
		}
	}
	slices.Sort(images)
	slices.Sort(videos)
	return images, videos, labels
}
