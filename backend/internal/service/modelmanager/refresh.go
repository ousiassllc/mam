package modelmanager

import (
	"context"
	"errors"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

// refreshReferenceWorkflows brings the models of the installed catalog models up to the templates' reference image support
// (#933): registerWorkflows copies a template only when the model is installed, so a model installed before the template
// learned reference images keeps the old text-to-image copy and would never accept a reference. Only a stored workflow with
// no reference mode whose template has one is replaced (the model files and the graph it points to are the same ones the
// catalog installs); the prices the user set are kept. Other edits of a model are left alone.
func (s *Service) refreshReferenceWorkflows(ctx context.Context) {
	view, err := s.Models.View()
	if err != nil {
		s.Logger.Warn("read the local models failed", "err", err)
		return
	}
	stored := map[string]localcfg.Model{}
	for _, m := range view.Models {
		if m.Endpoint == BundledEndpoint {
			stored[m.Name] = m.Model
		}
	}
	for id := range s.state.Get().Models {
		m, ok := s.cat.Model(id)
		if !ok {
			continue
		}
		for _, w := range m.Workflows {
			s.refreshWorkflow(ctx, w.Name, w.Template, stored)
		}
	}
}

func (s *Service) refreshWorkflow(ctx context.Context, name, template string, stored map[string]localcfg.Model) {
	cur, ok := stored[name]
	tpl, found := localcfg.TemplateByID(template)
	if !ok || !found || cur.Workflow == nil || tpl.Model.Workflow == nil {
		return
	}
	if cur.Workflow.ReferenceMode != "" || tpl.Model.Workflow.ReferenceMode == "" {
		return
	}
	next := tpl.Model
	next.CostPerImage, next.CostPerSecond = cur.CostPerImage, cur.CostPerSecond
	err := s.Models.UpdateModel(ctx, BundledEndpoint, name, localmodelsInput(name, next))
	if err != nil && !errors.Is(err, ErrPinned) {
		s.Logger.Warn("update the workflow of an installed model failed", "model", name, "err", err)
	}
}
