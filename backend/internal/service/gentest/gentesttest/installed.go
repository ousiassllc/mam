package gentesttest

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localbackends"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
)

// InstalledID is the model ID of the workflow the model manager creates for an installed catalog model of the bundled engine.
func InstalledID(name string) string { return localcfg.ID(localcfg.KindComfyUI, EndpointName, name) }

// WithInstalled adds the models a user would have installed from the model manager: each name (a workflow name of the catalog, "flux1-dev")
// is a model of the bundled endpoint made from its template (the template ID), with a fake backend. The models that were there stay.
func WithInstalled(templates map[string]string) Option {
	return func(r *Rig, d *gentest.Deps) {
		if r.Config.Endpoints == nil {
			r.Config.Endpoints = map[string]localcfg.Endpoint{}
		}
		if r.Config.LocalModels == nil {
			r.Config.LocalModels = map[string]localcfg.Model{}
		}
		r.Config.Endpoints[EndpointName] = localcfg.Endpoint{Kind: localcfg.KindComfyUI, BaseURL: "http://127.0.0.1:1"}
		for name, id := range templates {
			tpl, ok := localcfg.TemplateByID(id)
			if !ok {
				panic("gentesttest: the template " + id + " is gone")
			}
			r.Config.LocalModels[localcfg.Key(EndpointName, name)] = tpl.Model
			r.extra = append(r.extra, domain.ModelName(InstalledID(name)))
		}
		d.BundledEndpoint = EndpointName
		localbackends.Publish(r.Config)
		r.T.Cleanup(func() { localbackends.Publish(modeladapter.DefaultConfig()) })
	}
}

// WithHidden hides the models the predicate names, as the adult models are while the age gate is closed.
func WithHidden(hidden func(modelID string) bool) Option {
	return func(_ *Rig, d *gentest.Deps) { d.Hidden = hidden }
}

// WithPlatformWarning gives the service the warning of a model for this PC (the OS / GPU columns of the catalog).
func WithPlatformWarning(f func(ctx context.Context, modelID string) string) Option {
	return func(_ *Rig, d *gentest.Deps) { d.PlatformWarning = f }
}

// WithCatalogInfo gives the service what the model manager's catalog knows of a model (adult, the traits, the VRAM guideline), for the filter of the model list.
func WithCatalogInfo(f func(modelID string) (modelfilter.CatalogInfo, bool)) Option {
	return func(_ *Rig, d *gentest.Deps) { d.CatalogInfo = f }
}
