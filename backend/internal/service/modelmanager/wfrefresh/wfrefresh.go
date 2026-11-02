// Package wfrefresh brings the models of the managed ComfyUI endpoint up to the reference image support of the templates
// (#933). modelmanager copies a template into "local_models" only when a catalog model is installed, so a model installed before
// its template learned reference images keeps the old text-to-image copy and would never accept a reference. Only such an
// unedited old copy is replaced (see unedited): the template grew nodes and inputs and rewired links, a copy the user changed
// differs from it in a value.
package wfrefresh

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
)

// Models is the part of localmodels.Service that Run uses.
type Models interface {
	View() (localmodels.View, error)
	UpdateModel(ctx context.Context, endpoint, name string, in localmodels.ModelInput) error
}

// Run replaces the saved copy of a catalog workflow when it has no reference mode, its template has one and the copy is an
// unedited old version of the template. Such a model was created by an install (its name is a catalog workflow's); the graph,
// the model files it points to and the assignments become the ones the catalog installs and the prices the user set are kept.
// A model the user edited (a value of its graph differs from the template's), a model whose template has no reference support
// and one that is up to date are left alone. warn gets a failure; a pinned model file (ErrPinned) is not one.
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
	if cur.Workflow.ReferenceMode != "" || tpl.Model.Workflow.ReferenceMode == "" || !unedited(*cur.Workflow, *tpl.Model.Workflow) {
		return localcfg.Model{}, false
	}
	next := tpl.Model
	next.CostPerImage, next.CostPerSecond = cur.CostPerImage, cur.CostPerSecond
	return next, true
}

// unedited reports whether cur is an old copy of tpl that the user did not change: the output node is the same, every
// assignment of cur is one of tpl, every node of the graph of cur is in the graph of tpl with the same class, and every input
// of such a node is in tpl too, with the same value where cur has a literal. A link of cur may differ in tpl (the template
// rewired links for the reference images); tpl may have more nodes, more inputs and more assignments. A literal that differs
// (a model file, steps, cfg, a LoRA) is an edit, and the model is left alone.
func unedited(cur, tpl localcfg.Workflow) bool {
	if cur.OutputNode != tpl.OutputNode {
		return false
	}
	for _, a := range cur.Assignments {
		if !slices.Contains(tpl.Assignments, a) {
			return false
		}
	}
	cg, err := localcfg.ParseGraph(cur.Graph)
	if err != nil {
		return false
	}
	tg, err := localcfg.ParseGraph(tpl.Graph)
	if err != nil {
		return false
	}
	for id, cn := range cg {
		tn, ok := tg[id]
		if !ok || tn.ClassType != cn.ClassType {
			return false
		}
		for name, cv := range cn.Inputs {
			tv, ok := tn.Inputs[name]
			if !ok || (!localcfg.IsLink(cv) && !sameJSON(cv, tv)) {
				return false
			}
		}
	}
	return true
}

// sameJSON compares two JSON values by content, not by spelling.
func sameJSON(a, b json.RawMessage) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}
