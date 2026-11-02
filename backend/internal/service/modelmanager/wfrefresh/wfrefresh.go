// Package wfrefresh brings the models of the managed ComfyUI endpoint up to the reference image support of the templates
// (#933). modelmanager copies a template into "local_models" only when a catalog model is installed, so a model installed before
// its template learned reference images keeps the old text-to-image copy and would never accept a reference.
package wfrefresh

import (
	"context"
	"errors"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
)

// Models is the part of localmodels.Service that Run uses.
type Models interface {
	View() (localmodels.View, error)
	UpdateModel(ctx context.Context, endpoint, name string, in localmodels.ModelInput) error
}

// Run replaces the saved copy of a catalog workflow when it has no reference mode and its template has one. Such a model was
// created by an install (its name is a catalog workflow's), and the graph, the model files it points to and the assignments are
// the ones the catalog installs; the prices the user set are kept. A model the user edited otherwise, a model whose template
// has no reference support and one that is up to date are left alone. warn gets a failure; a pinned model file (ErrPinned) is
// not one.
func Run(ctx context.Context, models Models, cat *catalog.Catalog, endpoint string, warn func(msg string, args ...any)) {
	view, err := models.View()
	if err != nil {
		warn("read the local models failed", "err", err)
		return
	}
	stored := map[string]localcfg.Model{}
	for _, m := range view.Models {
		if m.Endpoint == endpoint {
			stored[m.Name] = m.Model
		}
	}
	for _, m := range cat.Models {
		for _, w := range m.Workflows {
			if next, ok := refreshed(stored[w.Name], w.Template); ok {
				err := models.UpdateModel(ctx, endpoint, w.Name, localmodels.ModelInput{Endpoint: endpoint, Name: w.Name, Model: next})
				if err != nil && !errors.Is(err, localmodels.ErrPinned) {
					warn("update the workflow of an installed model failed", "model", w.Name, "err", err)
				}
			}
		}
	}
}

// refreshed is the model to save over cur: the template's, with the prices of cur. ok is false when nothing is to change.
func refreshed(cur localcfg.Model, template string) (localcfg.Model, bool) {
	tpl, found := localcfg.TemplateByID(template)
	if !found || cur.Workflow == nil || tpl.Model.Workflow == nil {
		return localcfg.Model{}, false
	}
	if cur.Workflow.ReferenceMode != "" || tpl.Model.Workflow.ReferenceMode == "" {
		return localcfg.Model{}, false
	}
	next := tpl.Model
	next.CostPerImage, next.CostPerSecond = cur.CostPerImage, cur.CostPerSecond
	return next, true
}
